package dynaphore

// A Dynaphore is a dynamically-sized semaphore.
type Dynaphore interface {
	// SetMax can be used to increase or decrease the maximum permitted concurrent locks granted by the Dynaphore.
	// Increases to the max will immediately unlock any currently blocked Up() calls.
	// Decreases to max will not interfere with existing locks granted, but will prevent any further Up() calls from
	// acquiring a lock until the current number of locks drops below the new max.
	// SetMax can be called concurrently with other methods, even with itself.
	SetMax(newMax int) Dynaphore

	// Current returns the current number of active locks
	Current() int

	// Up attempts to acquire a lock.  If the current number of active locks is less than the set maximum, Up() will
	// immediately return.  If not, Up() will block until a lock can be acquired.
	Up() Dynaphore

	// Down releases a previously acquired lock.  It should not be called without first having called Up().  A common
	// method of usage is:
	//   defer dyn.Up().Down()
	// This will immediately acquire a lock (or block until it can), then defer the release of that lock.
	Down() Dynaphore

	// UpChan() is an alternative to Up().  It returns a receive-only channel that will be closed when a lock is
	// acquired.  It exists to allow a lock to be acquired within the context of a Select statement, to allow options
	// like a timeout on lock attempts.  The lock must still be released via Down once acquired.
	// Note: Down should *not* be called unless the lock is actually acquired.  To abandon waiting for the lock instead,
	// the caller should use DownChan().
	UpChan() LockChan

	// DownChan is the companion of UpChan().  If UpChan() is called, but the caller abandons waiting for the lock
	// (for example, due to timeout), DownChan() should be called with the channel UpChan() returned.  DownChan will
	// then wait for the LockChan to be closed, then call Down, removing the need for the Caller to do so.
	// DownChan can also be used unconditionally as the "Down()" version of an UpChan() call.  Calls to DownChan() after
	// UpChan has acquired its lock are semantically identical to calling Down().
	DownChan(lockCh LockChan)
}

type LockChan <-chan struct{}

type dynaphore struct {
	lock    chan struct{} // the dynaphore sends on this to gain a lock
	unlock  chan struct{} // the dynaphore sends on this go release a lock
	max     chan int      // the dynaphore sends on this to indicate that the maximum has changed
	current chan int      // the dynyaphore receives on this when it wants to know the current number of locks
}

func NewDynaphore(max int) Dynaphore {
	s := dynaphore{
		lock:   make(chan struct{}),
		unlock: make(chan struct{}, 1),
		max:    make(chan int, 1),
	}
	s.max <- max

	go s.manager()

	return &s
}

func (s *dynaphore) SetMax(newMax int) Dynaphore {
	s.max <- newMax
	return s
}

func (s *dynaphore) Current() int {
	return <-s.current
}

func (s *dynaphore) Up() Dynaphore {
	s.lock <- struct{}{}
	return s
}

func (s *dynaphore) UpChan() LockChan {
	l := make(chan struct{})
	go func() {
		s.lock <- struct{}{}
		close(l)
	}()
	return l
}

func (s *dynaphore) DownChan(l LockChan) {
	go func() {
		<-l
		s.Down()
	}()
}

func (s *dynaphore) Down() Dynaphore {
	s.unlock <- struct{}{}
	return s
}

func (s *dynaphore) manager() {
	current := 0
	max := <-s.max
	for {
		lock := s.lock
		if current >= max {
			lock = nil // at or over max, block locks until we are under
		}
		select {
		case <-lock:
			current++
		case <-s.unlock:
			if current > 0 { // this is to handle misbehaving users that call Down without having called Up first
				current--
			}
		case s.current <- current: // respond to a Current() call
		case max = <-s.max: //update max, then loop
		}
	}
}
