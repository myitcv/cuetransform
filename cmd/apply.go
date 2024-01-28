// Copyright 2024 The CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"github.com/spf13/cobra"
)

const (
	flagPath flagName = "path"
)

// newApplyCmd creates a new runtrybot command
func newApplyCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "apply a transform to some data",
		RunE:  mkRunE(c, runApply),
	}
	cmd.Flags().StringArrayP(string(flagPath), "l", nil, "CUE expression for single path component (see 'cue help flags' for details)")
	return cmd
}

type applyCtx struct {
	cmd  *Command
	args []string
	ctx  *cue.Context
}

func runApply(cmd *Command, args []string) error {
	a := applyCtx{
		cmd:  cmd,
		args: args,
		ctx:  cuecontext.New(),
	}
	return a.run()
}

func (a *applyCtx) run() error {
	// For now we only support a single instance
	bps := load.Instances(a.args, nil)
	if l := len(bps); l != 1 {
		return fmt.Errorf("must load exactly one instance; loaded %d", l)
	}
	bp := bps[0]
	if bp.Err != nil {
		return bp.Err
	}
	v := a.ctx.BuildInstance(bp)
	if err := v.Err(); err != nil {
		return err
	}

	// Locate the data and split into a slice of leaf values
	data := v.LookupPath(cue.MakePath(cue.Str("data")))
	if err := data.Err(); err != nil {
		return err
	}
	if err := data.Validate(cue.Concrete(true)); err != nil {
		return fmt.Errorf("data must be concrete: %w", err)
	}
	// TODO: add a method to "strip" a leading path element from a value?
	data = a.ctx.CompileString("{}").FillPath(cue.Path{}, data)

	// Locate the transforms. We will validate each as we go
	transforms := v.LookupPath(cue.MakePath(cue.Str("transforms")))
	if err := transforms.Err(); err != nil {
		return err
	}
	transFormsIter, err := transforms.List()
	if err != nil {
		return err
	}
	for transFormsIter.Next() {
		t := transFormsIter.Value()
		typ, err := t.LookupPath(cue.MakePath(cue.Str("type"))).String()
		if err != nil {
			return err
		}

		switch typ {
		case "delete":
			// We only perform the delete if the path field exists. If it doesn't,
			// that is an indicator that whatever logic the author intended to guard
			// the transform has not been satisfied.
			p := t.LookupPath(cue.MakePath(cue.Str("path")))
			if !p.Exists() {
				continue
			}

			// We must ensure that the reference from path is to a position within
			// the data field within the current transform
			_, ref := p.ReferencePath()
			currDataElems := append(t.Path().Selectors(), cue.Str("data"))
			currDataPath := cue.MakePath(currDataElems...)
			targetRefElems := ref.Selectors()
			if len(targetRefElems) <= len(currDataElems) {
				return fmt.Errorf("%v is not a reference into %v", ref, currDataPath)
			}
			for i, v := range currDataElems {
				if v != targetRefElems[i] {
					return fmt.Errorf("%v is not a reference into %v", ref, currDataPath)
				}
			}
			dataRefElems := targetRefElems[len(currDataElems):]
			dataRef := cue.MakePath(dataRefElems...)

			// We know it's a reference into the current data now. Now apply
			// the transform.
			//
			// A delete path must exist. We work inwards to outwards, adjusting
			// structs and lists as we go.
			//
			// An insert path does not need to exist. Unification takes care of
			// everything up to the last element of the insert path. If the last
			// element is an index, the insert causes any existing elements at
			// and above that point to be "shifted right".

			if !data.LookupPath(dataRef).Exists() {
				return fmt.Errorf("%v: failed to find data to delete at %v", t.Path(), dataRef)
			}

			// First create a new container with the field/index removed
			var accum cue.Value
			toRemove, container := dataRefElems[len(dataRefElems)-1], dataRefElems[:len(dataRefElems)-1]
			containerPath := cue.MakePath(container...)
			switch toRemove.Type() {
			case cue.IndexLabel:
				// We want to leave an empty list in case we remove the last
				// element
				newList := a.ctx.CompileString("[...]")
				list, err := data.LookupPath(containerPath).List()
				if err != nil {
					return fmt.Errorf("%v: expected list at path %v: %v", t.Path(), containerPath, err)
				}
				var i int
				for list.Next() {
					if list.Selector() == toRemove {
						continue
					}
					newList = newList.FillPath(cue.MakePath(cue.Index(i)), list.Value())
					i++
				}
				accum = newList
			case cue.StringLabel:
				// we want to leave an empty struct in case we remove the last
				// field
				newStruct := a.ctx.CompileString("{}")
				strct, err := data.LookupPath(containerPath).Fields()
				if err != nil {
					return fmt.Errorf("%v: expected struct at path %v: %v", t.Path(), containerPath, err)
				}
				for strct.Next() {
					if strct.Selector() == toRemove {
						continue
					}
					newStruct = newStruct.FillPath(cue.MakePath(strct.Selector()), strct.Value())
				}
				accum = newStruct
			default:
				return fmt.Errorf("%v: unknown selector type %v", t.Path(), toRemove.Type())

			}

			// Now update all containers up to and including root
			var last cue.Selector
			for len(container) > 0 {
				final := len(container) - 1
				last, container = container[final], container[:final]
				containerPath := cue.MakePath(container...)

				switch last.Type() {
				case cue.IndexLabel:
					newList := a.ctx.CompileString("[...]")
					list, err := data.LookupPath(containerPath).List()
					if err != nil {
						return fmt.Errorf("%v: expected list at path %v: %v", t.Path(), containerPath, err)
					}
					for i := 0; list.Next(); i++ {
						var val cue.Value
						if list.Selector() == last {
							val = accum
						} else {
							val = list.Value()
						}
						newList = newList.FillPath(cue.MakePath(cue.Index(i)), val)
					}
					accum = a.ctx.Encode(newList)
				case cue.StringLabel:
					newStruct := a.ctx.CompileString("{}")
					strct, err := data.LookupPath(containerPath).Fields()
					if err != nil {
						return fmt.Errorf("%v: expected struct at path %v: %v", t.Path(), containerPath, err)
					}
					for strct.Next() {
						elemPath := cue.MakePath(strct.Selector())
						if strct.Selector() == last {
							newStruct = newStruct.FillPath(elemPath, accum)
						} else {
							newStruct = newStruct.FillPath(elemPath, strct.Value())
						}
					}
					accum = newStruct
				default:
					return fmt.Errorf("%v: unknown selector type %v", t.Path(), last.Type())
				}
			}
			data = accum

		case "unify":
			// value must exist and be concrete
			val := t.LookupPath(cue.MakePath(cue.Str("value")))
			if !val.Exists() {
				return fmt.Errorf("%v: no value to insert", t.Path())
			}
			if err := val.Validate(cue.Concrete(true)); err != nil {
				return fmt.Errorf("%v: value is not concrete: %v", t.Path(), err)
			}

			data = data.Unify(val)

		default:
			return fmt.Errorf("%v: don't know how to handle transform type %v", t.Path(), typ)
		}

		// Ensure we have concrete data
		if err := data.Validate(cue.Concrete(true)); err != nil {
			return fmt.Errorf("%v: transform left non-concrete value: %v", t.Path(), err)
		}
	}

	// TODO: support output in different formats, and also handle cases where we
	// don't have a struct top-level
	n := data.Syntax(cue.Concrete(true)).(*ast.StructLit)
	f := &ast.File{
		Decls: n.Elts,
	}
	res, err := format.Node(f)
	if err != nil {
		return fmt.Errorf("failed format data for output: %w", err)
	}
	fmt.Printf("%s", res)

	return nil
}
