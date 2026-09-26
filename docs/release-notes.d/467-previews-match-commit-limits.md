- Previews now refuse what the operation they preview would refuse (#467).
  The packet preview reports a selection that together exceeds the packet's
  limits, or whose retained execution does not match its case's original
  bytes, which before was refused only when the packet was assembled. The
  project retirement preview refuses a project one backup cannot hold, which
  before was refused only once the archive or delete ran. Each refusal reads
  as it did before.
