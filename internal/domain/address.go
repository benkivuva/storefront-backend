package domain

import (
"errors"
"fmt"
"strings"
"time"
)

var (
ErrInvalidRecipient  = errors.New("domain: invalid address recipient")
ErrInvalidLine1      = errors.New("domain: invalid address line 1")
ErrInvalidTown       = errors.New("domain: invalid address town")
ErrZeroAddressID     = errors.New("domain: address id is zero")
ErrZeroAddressUserID = errors.New("domain: address user id is zero")
ErrZeroAddressTime   = errors.New("domain: address timestamp is zero")
)

const (
maxRecipientLen = 100
maxLine1Len     = 200
maxLine2Len     = 200
maxTownLen      = 100
maxCountyLen    = 100
maxNotesLen     = 500
)

// Address is an immutable shipping address. There is no With* method —
// editing an address would silently change the shipping destination of past
// orders that reference it. Customers who want a different address create a
// new one.
type Address struct {
id        ID
userID    ID
recipient string
phoneE164 string
line1     string
line2     string
town      string
county    string
notes     string
createdAt time.Time
}

// AddressRecord is the argument shape for RehydrateAddress.
type AddressRecord struct {
ID        ID
UserID    ID
Recipient string
PhoneE164 string
Line1     string
Line2     string
Town      string
County    string
Notes     string
CreatedAt time.Time
}

// NewAddress validates and constructs an Address. recipient, phoneE164,
// line1, and town are required; line2, county, and notes may be empty.
func NewAddress(
id, userID ID,
recipient, phoneE164, line1, line2, town, county, notes string,
at time.Time,
) (Address, error) {
if id.IsZero() {
return Address{}, ErrZeroAddressID
}
if userID.IsZero() {
return Address{}, ErrZeroAddressUserID
}
if at.IsZero() {
return Address{}, ErrZeroAddressTime
}

rec, err := required("recipient", recipient, maxRecipientLen, ErrInvalidRecipient)
if err != nil {
return Address{}, err
}
phone, err := normalizePhone(phoneE164)
if err != nil {
return Address{}, err
}
if phone == "" {
return Address{}, fmt.Errorf("%w: phone is required for an address", ErrInvalidPhone)
}
l1, err := required("line1", line1, maxLine1Len, ErrInvalidLine1)
if err != nil {
return Address{}, err
}
town, err = required("town", town, maxTownLen, ErrInvalidTown)
if err != nil {
return Address{}, err
}

return Address{
id:        id,
userID:    userID,
recipient: rec,
phoneE164: phone,
line1:     l1,
line2:     optional(line2, maxLine2Len),
town:      town,
county:    optional(county, maxCountyLen),
notes:     optional(notes, maxNotesLen),
createdAt: at,
}, nil
}

// RehydrateAddress rebuilds an Address from persisted state.
func RehydrateAddress(r AddressRecord) (Address, error) {
return NewAddress(
r.ID, r.UserID,
r.Recipient, r.PhoneE164, r.Line1, r.Line2, r.Town, r.County, r.Notes,
r.CreatedAt,
)
}

func required(field, s string, max int, sentinel error) (string, error) {
s = strings.TrimSpace(s)
if s == "" {
return "", fmt.Errorf("%w: %s is required", sentinel, field)
}
if len(s) > max {
return "", fmt.Errorf("%w: %s length %d > %d", sentinel, field, len(s), max)
}
return s, nil
}

func optional(s string, max int) string {
s = strings.TrimSpace(s)
if len(s) > max {
s = s[:max]
}
return s
}

func (a Address) ID() ID               { return a.id }
func (a Address) UserID() ID           { return a.userID }
func (a Address) Recipient() string    { return a.recipient }
func (a Address) PhoneE164() string    { return a.phoneE164 }
func (a Address) Line1() string        { return a.line1 }
func (a Address) Line2() string        { return a.line2 }
func (a Address) Town() string         { return a.town }
func (a Address) County() string       { return a.county }
func (a Address) Notes() string        { return a.notes }
func (a Address) CreatedAt() time.Time { return a.createdAt }
