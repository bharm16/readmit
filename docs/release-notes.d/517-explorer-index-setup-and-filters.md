- The case explorer now shows its messages first (#517). Index creation and
  rebuild open in an explicit setup view (Set up index, Set up rebuild) that
  writes nothing until Build index or Rebuild index is pressed, and a rebuild
  keeps the index's own fields, stored content, retention end and file instead
  of turning a finite deadline into indefinite retention. The retention end is
  entered in local time with the UTC instant it is stored as shown beside it,
  and an incomplete time blocks the build. Replacement applies only to the
  selected index of the open case. The filter editor opens from New filter,
  Save and apply filter both saves and selects, and Discard filter draft clears
  only the unsaved form. Paging and closing the command palette are named icon
  buttons, and grid rows stay aligned with the scrollbar at every text size.
  A failed draft save offers Retry draft save, Keep as new draft or Discard
  draft only where that editor really provides the action, and a refused
  discard keeps the text on screen.
