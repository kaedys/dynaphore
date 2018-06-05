# Dynaphore
A dynamically-sized dynaphore library.  

Like a dynaphore, a dynaphore allows a resource to be accessed by a limited quantity of concurrent operations. 
Example use cases:
  * No more than 100 concurrent database connections.
  * No more than 10 concurrent file writers.

If the dynaphore is at its maximum capacity, `Up()` will block until a lock is released via `Down()` in another goroutine.

Dynaphores can be dynamically and concurrently resized via the `SetMax(newMax)` method.
 * If `newMax` is higher the the current max, goroutines waiting on a lock will be unblocked up to `newMax`.
 * If `newMax` is lower then the current max, any existing goroutines holding locks will continue to run until complete, 
 but no new locks will be granted until the active count is below `newMax`.
 * Resizing the dynaphore's maximum in either direction can be done at any time, and is race safe regardless of the 
 number of locks held or goroutines waiting for a lock.

All methods return the Dynaphore they were called on to allow daisychaining calls.  This is particularly useful when
deferring calls, since only the last function call in a daisychain is deferred.  Thus in the statement:
```
defer dyn.Up().Down()
```
`Up()` is called immediately, establishing a lock (or blocking until it can), and `Down()` is deferred to release it.

Example of usage:

```golang
dyn := NewDynaphore(10)

go func(dyn *Dynaphore) {
  defer dyn.Up().Down() // acquires a lock immediately, then defers the release of that lock.

  db := sql.Open(...)
  // ...
  db.Close()
}(dyn)
```

The dynaphore also defines two channel-based methods: `UpChan() LockChan` and `DownChan(l LockChan)`.  These methods
are designed to allow the dynaphore to function within the context of a `select` statement, to allow functionality 
such as a timeout.  
* `UpChan()` will return a channel that will block until a lock is acquired, and will then be closed (allowing all 
further receive operations on that channel to succeed).  Once this lock is acquired, it must be released via either 
`Down()` or `DownChan()`, as normal.  
* `DownChan()` allows an attempted lock via `UpChan()` to be aborted without leaving dangling locks.  If the caller
aborts listening to the channel returned by `UpChan()` (for example, due to a timeout), `DownChan()` should be called
with that channel, which will ensure the eventual lock is released.  `DownChan()` can also be used unconditionally as
the version of `Down()` when using `UpChan()`, as calls to `DownChan()` after `UpChan()` acquires its lock are 
semantically identically to simply calling `Down()`.

Example of usage:

```golang
dyn := NewDynaphore(10)
timeout := time.Minute

go func(dyn *Dynaphore) {
  lockCh := dyn.UpChan() // starts attempting to acquire a lock.  Receives from lockCh will succeed once acquired.
  defer dyn.DownChan(lockCh) // releases the resource if acquired, or ensures it will be released if we timed out.
  
  select {
  case <-lockCh: // lock acquired, do our stuff.
    db := sql.Open(...)
    // ...
    db.Close()
  case <-time.After(timeout):
    log.Printf("Could not acquire a database connection within %v, aborting.", timeout)
  }
}(dyn)
```