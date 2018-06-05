package dynaphore

import (
	"testing"
	"time"
)

func TestDynaphore(t *testing.T) {
	dyn := NewDynaphore(3)

	dyn.Up() // 1
	dyn.Up() // 2
	dyn.Up() // 3

	start := time.Now()
	delay := 100 * time.Millisecond

	go func() {
		// Sleep a bit, then release a lock, so we can make sure that Up() blocks as expected, but then releases.
		time.Sleep(delay)
		dyn.Down()
	}()

	dyn.Up() // should block

	tolerance := 5 * time.Millisecond
	dur := time.Now().Sub(start)
	if dur > delay+tolerance || dur < delay-tolerance {
		t.Fatalf("Lock delay expected to within %v of %v, actual %v.", tolerance, delay, dur)
	}

	dyn.Down() // 2
	dyn.Down() // 1
	dyn.Down() // 0
	dyn.Down() // 0, should effectively no-op
}

func TestDynaphore_Chans(t *testing.T) {
	dyn := NewDynaphore(3)

	dyn.Up() // 1
	dyn.Up() // 2
	dyn.Up() // 3

	lockCh := dyn.UpChan()

	select {
	case <-lockCh: // channel closed, fail
		t.Fatalf("First lock unexpectedly acquired.")
	default: // channel not closed, as expected
	}

	dyn.Down() // release a lock, allowing the UpChan lock to be acquired

	// the lockCh is closed in a goroutine, give it a moment to actually execute
	time.Sleep(5 * time.Millisecond)

	select {
	case <-lockCh: // channel closed, as expected
	default: // channel not closed, fail
		t.Fatalf("First lock could not be acquired.")
	}

	// try to get another lock, to make sure we're still maxed at 3 locks out
	lockCh2 := dyn.UpChan()

	select {
	case <-lockCh2: // channel closed, fail
		t.Fatalf("Second lock unexpectedly acquired.")
	default: // channel not closed, as expected
	}

	// set up a triggered release
	dyn.DownChan(lockCh2)

	// release the lock, which should trigger the above UpChan lock to be acquired and immediately released by DownChan()
	dyn.Down()

	// We should only have 2 locks now, so try to acquire another one to make sure the above release worked.
	lockCh3 := dyn.UpChan()
	time.Sleep(5 * time.Millisecond) // give a moment to let the goroutines execute

	select {
	case <-lockCh3: // channel immediately closed because lock was immediately available, as expected
	default: // channel not closed, fail
		t.Fatalf("Third lock could not be acquired.")
	}
}

func TestDynaphore_SetMax(t *testing.T) {
	dyn := NewDynaphore(1)

	dyn.Up()

	lockCh := dyn.UpChan()
	lockCh2 := dyn.UpChan()

	select {
	case <-lockCh: // channel closed, fail
		t.Fatalf("First lock unexpectedly acquired.")
	case <-lockCh2: // channel closed, fail
		t.Fatalf("Second lock unexpectedly acquired.")
	default: // channels not closed, as expected
	}

	dyn.SetMax(3) // should release both locks

	// the lockCh is closed in a goroutine, give it a moment to actually execute
	time.Sleep(5 * time.Millisecond)

	select {
	case <-lockCh: // channel closed, as expected
	default: // channel not closed, fail
		t.Fatalf("First lock could not be acquired.")
	}

	select {
	case <-lockCh2: // channel closed, as expected
	default: // channel not closed, fail
		t.Fatalf("Second lock could not be acquired.")
	}

	// We now have 3 locks acquired, and a max of 3.  Let's reduce the max and make sure that works too.
	dyn.SetMax(2)

	lockCh3 := dyn.UpChan()
	select {
	case <-lockCh3: // channel closed, fail
		t.Fatalf("Third lock unexpectedly acquired after no Down() calls.")
	default: // channel not closed, as expected
	}

	dyn.Down() // now have 2 locks, *still* shouldn't be able to get a new one
	time.Sleep(5 * time.Millisecond)

	select {
	case <-lockCh3: // channel closed, fail
		t.Fatalf("Third lock unexpectedly acquired after one Down() calls.")
	default: // channel not closed, as expected
	}

	dyn.Down() // now have 1 lock, should be able to get a new one now
	time.Sleep(5 * time.Millisecond)

	select {
	case <-lockCh3: // channel closed, as expected
	default: // channel not closed, fail
		t.Fatalf("Third lock could not be acquired after two Down() calls.")
	}
}

func TestDynaphore_Defer(t *testing.T) {
	dyn := NewDynaphore(1)

	// synchronization channels for the goroutine
	started := make(chan struct{})
	finish := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished) // defers are run first-in last-out, so this happens *after* Down() completes
		defer dyn.Up().Down() // should acquire a lock, but not release it until this function returns
		close(started) // let the main routine know we've got the lock
		<-finish // wait until the main routine tells us to finish
	}()

	<-started // wait for the goroutine to indicate it has the lock

	// Shouldn't be able to acquire a lock yet
	lockCh := dyn.UpChan()
	select {
	case <-lockCh: // channel closed, fail
		t.Fatalf("Lock unexpectedly acquired.")
	default: // channel not closed, as expected
	}

	// tell the goroutine to return and run its defers, and wait until it has called Down()
	close(finish)
	<-finished

	// the lockCh is closed in a goroutine, give it a moment to actually execute
	time.Sleep(5 * time.Millisecond)

	select {
	case <-lockCh: // channel closed, as expected
	default: // channel not closed, fail
		t.Fatalf("Lock could not be acquired.")
	}
}