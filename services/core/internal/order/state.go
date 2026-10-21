package order

const (
	StatusCreated        = "CREATED"
	StatusPaymentPending = "PAYMENT_PENDING"
	StatusPaid           = "PAID"
	StatusShipped        = "SHIPPED"
	StatusCompleted      = "COMPLETED"
	StatusCanceled       = "CANCELED"

	OpCreate = "CREATE_ORDER"
	OpUpdate = "UPDATE_ORDER"
)

func NextStatus(current string) (string, bool) {
	switch current {
	case StatusCreated:
		return StatusPaymentPending, true
	case StatusPaymentPending:
		return StatusPaid, true
	case StatusPaid:
		return StatusShipped, true
	case StatusShipped:
		return StatusCompleted, true
	default:
		return "", false
	}
}

func CanCancel(status string) bool {
	return status == StatusCreated || status == StatusPaymentPending
}

func CanUpdate(status string) bool {
	return status == StatusCreated
}
