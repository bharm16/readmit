- Profile evaluation no longer treats a conditional HL7 v2 field or component
  as optional. A profile pack can now state each element's edition usage,
  its condition (such as "HD.3 is required when HD.2 is valued"), its length
  bounds and its table's kind, and evaluation reports a missing or
  prohibited conditional element with its original byte offsets. A condition
  that depends on information the message does not carry is reported as
  undecided, never passed. Evaluations of existing packs whose optional
  base declarations may hide conditional requirements now report those
  declarations as unsupported instead of passing; stored results are
  unchanged.
