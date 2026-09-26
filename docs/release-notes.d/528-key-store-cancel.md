- Cancelling an operation that is reading a key from a declared key store, or
  that store's five-second limit running out, now ends the read within about a
  second even when the store's program started a slow program of its own
  (#528). Before, the read waited until that second program finished.
