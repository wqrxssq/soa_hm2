package identity

const (
	RoleUser   = "USER"
	RoleSeller = "SELLER"
	RoleAdmin  = "ADMIN"
)

type Actor struct {
	ID   string
	Role string
}

func (a Actor) IsAdmin() bool  { return a.Role == RoleAdmin }
func (a Actor) IsSeller() bool { return a.Role == RoleSeller }
func (a Actor) IsUser() bool   { return a.Role == RoleUser }
