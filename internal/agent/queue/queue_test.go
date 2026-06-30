package queue

import "testing"

func job(id string) Job { return Job{Source: "claude_code", Scheme: "prompt_id", ID: id} }

func TestOfferDedupBySameKey(t *testing.T) {
	q := New(10)
	if !q.Offer(job("A")) {
		t.Fatal("first offer should accept")
	}
	if q.Offer(job("A")) {
		t.Fatal("duplicate key should be shed")
	}
}

func TestOfferShedsWhenFull(t *testing.T) {
	q := New(1)
	if !q.Offer(job("A")) {
		t.Fatal("first should accept")
	}
	if q.Offer(job("B")) {
		t.Fatal("over-capacity offer should be shed")
	}
	if q.Dropped() != 1 {
		t.Fatalf("Dropped = %d, want 1", q.Dropped())
	}
}

func TestNextReturnsOfferedJob(t *testing.T) {
	q := New(10)
	q.Offer(job("A"))
	got, ok := q.Next()
	if !ok || got.ID != "A" {
		t.Fatalf("Next = (%+v,%v)", got, ok)
	}
}

func TestNextUnblocksOnClose(t *testing.T) {
	q := New(10)
	go q.Close()
	if _, ok := q.Next(); ok {
		t.Fatal("Next after close should return ok=false")
	}
}
