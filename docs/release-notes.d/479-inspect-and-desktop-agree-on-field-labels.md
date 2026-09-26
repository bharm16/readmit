- Inspecting a message whose MSH-12 carries a repeated version, such as
  `2.5.1~2.9`, now names its fields with the bundled 2.5.1 labels in
  `readmit inspect` exactly as the desktop inspector already did: the
  declared version is read once, at the first component of the first
  repetition, by one shared reader instead of each surface reading MSH-12 in
  its own way (#479). Before, the command reported such a message as
  positional only while the desktop labelled the same fields.
