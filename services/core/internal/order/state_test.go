package order

import "testing"

func TestNextStatus(t *testing.T) {
	steps := []string{StatusCreated, StatusPaymentPending, StatusPaid, StatusShipped, StatusCompleted}
	for i := 0; i < len(steps)-1; i++ {
		got, ok := NextStatus(steps[i])
		if !ok || got != steps[i+1] {
			t.Fatalf("NextStatus(%s) = %s, %v", steps[i], got, ok)
		}
	}
	if _, ok := NextStatus(StatusCompleted); ok {
		t.Fatal("completed order must not advance")
	}
	if _, ok := NextStatus(StatusCanceled); ok {
		t.Fatal("canceled order must not advance")
	}
}

func TestCancelAndUpdateGuards(t *testing.T) {
	if !CanCancel(StatusCreated) || !CanCancel(StatusPaymentPending) {
		t.Fatal("cancel must be allowed from CREATED and PAYMENT_PENDING")
	}
	if CanCancel(StatusPaid) || CanCancel(StatusShipped) || CanCancel(StatusCompleted) || CanCancel(StatusCanceled) {
		t.Fatal("cancel must be rejected outside CREATED and PAYMENT_PENDING")
	}
	if !CanUpdate(StatusCreated) || CanUpdate(StatusPaymentPending) {
		t.Fatal("update must be allowed only in CREATED")
	}
}
