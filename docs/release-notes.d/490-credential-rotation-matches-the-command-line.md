- The desktop's credential references now show the rotation state
  `readmit secret show` reports, decided by the same Go rule instead of the
  window's own reading of the maximum age (#490). Before, a compound age such
  as `1h30m` was shown as unreadable, and a zero age as overdue where the
  command line reports no rotation declared.
