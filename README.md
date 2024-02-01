## `cuetransform` - an experiment in transforming data (and schema) using CUE

This repo exists to experiment with some ideas around data and schema
transformation, where CUE is used to describe the transformation.

The experiment is implemented in the form of a command,
`github.com/cue-exp/cuetransform`. It has a single subcommand `apply` which is
used to apply a series of transformations to some concrete data.

Some notes:

* This experiment is limited to concrete data (for now). But it's very much in
  scope to consider how it could be applied to schema.
* Every transformation after the first transformation is effectively limited to
  checking against the `_` constraint. Which is not very helpful, and not really
  in the spirit of CUE. Later versions of this experiment will look to work with
  transforming schemas, so that subsequent transformations can be vetted (using
  a special static check). If we ever get to a position of supporting something
  like delete as a builtin, then regular `cue vet` would be sufficient to check
  a sequence of transformations (noting they would not be supplied as a list,
  rather a sequence of expressions).

## Ok, so can the experiment do?

The main place to look for an understanding of what it can/can't do are the
`testscript` tests in the [tests directory](cmd/testdata/script/).
The [basic test](cmd/testdata/script/basic.txt) is a good starting
point.

## Context/background

TODO: write this section.

## Status

* Very much work-in-progress.
* Code is very hacky - the result of a train journey's worth of thinking and
  effort.
* Lots of context missing in this README.

## Feedback

Please raise [issues](https://github.com/myitcv/cuetransform/issues) with any
feedback.
