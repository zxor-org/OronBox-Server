package coordinator

import (
	"errors"
	"testing"
)

type publicationResult struct {
	rows int64
	err  error
}

func (result publicationResult) LastInsertId() (int64, error) { return 0, errors.New("unsupported") }
func (result publicationResult) RowsAffected() (int64, error) { return result.rows, result.err }

func TestRequirePublicationLease(t *testing.T) {
	if err := requirePublicationLease(publicationResult{rows: 1}, "publication"); err != nil {
		t.Fatalf("valid lease result: %v", err)
	}
	if err := requirePublicationLease(publicationResult{}, "publication"); err == nil {
		t.Fatal("lost lease was accepted")
	}
	want := errors.New("rows failed")
	if err := requirePublicationLease(publicationResult{err: want}, "publication"); !errors.Is(err, want) {
		t.Fatalf("RowsAffected error = %v, want %v", err, want)
	}
}
