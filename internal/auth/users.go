package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

// Roles, in increasing order of access.
const (
	RoleNone   = "none"
	RoleViewer = "viewer"
	RoleEditor = "editor"
	RoleAdmin  = "admin"
)

var roleRank = map[string]int{RoleNone: 0, RoleViewer: 1, RoleEditor: 2, RoleAdmin: 3}

// AtLeast reports whether role grants at least min.
func AtLeast(role, min string) bool { return roleRank[role] >= roleRank[min] }

var ErrNotFound = errors.New("user not found")

// InputError is a rejected request (maps to HTTP 400).
type InputError struct{ Msg string }

func (e *InputError) Error() string { return e.Msg }

func badInput(format string, args ...any) error {
	return &InputError{Msg: fmt.Sprintf(format, args...)}
}

// User is a parsec account. With oidc sign-in, accounts are created on
// first sign-in; with local sign-in, an admin creates them.
type User struct {
	Username     string `yaml:"username" json:"username"`
	Name         string `yaml:"name,omitempty" json:"name"`
	Email        string `yaml:"email,omitempty" json:"email"`
	Role         string `yaml:"role" json:"role"`
	PasswordHash string `yaml:"passwordHash,omitempty" json:"-"`
	LastSeen     string `yaml:"lastSeen,omitempty" json:"lastSeen"`
	// Fixed is set (in responses only) for admins listed in the config file,
	// whose role cannot be changed in the app.
	Fixed bool `yaml:"-" json:"fixed"`
	// HasPassword is set in responses for local accounts.
	HasPassword bool `yaml:"-" json:"hasPassword"`
}

// DisplayName falls back to the username.
func (u User) DisplayName() string {
	if u.Name != "" {
		return u.Name
	}
	return u.Username
}

// Settings are adjustable from the app by admins.
type Settings struct {
	DefaultRole string `yaml:"defaultRole" json:"defaultRole"`
}

type usersFile struct {
	Settings Settings `yaml:"settings"`
	Users    []User   `yaml:"users"`
}

// Users is the account list in <stateDir>/users.yaml.
type Users struct {
	path   string
	admins map[string]bool

	mu   sync.Mutex
	data usersFile
}

var validUsername = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@+-]{0,63}$`)

func OpenUsers(stateDir string, admins []string, defaultRole string) (*Users, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	u := &Users{path: filepath.Join(stateDir, "users.yaml"), admins: map[string]bool{}}
	for _, a := range admins {
		u.admins[strings.ToLower(a)] = true
	}
	b, err := os.ReadFile(u.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		u.data.Settings.DefaultRole = defaultRole
	case err != nil:
		return nil, err
	default:
		if err := yaml.Unmarshal(b, &u.data); err != nil {
			return nil, fmt.Errorf("%s: %w", u.path, err)
		}
		if u.data.Settings.DefaultRole == "" {
			u.data.Settings.DefaultRole = defaultRole
		}
	}
	return u, nil
}

func (u *Users) view(x User) User {
	if u.admins[strings.ToLower(x.Username)] {
		x.Role = RoleAdmin
		x.Fixed = true
	}
	if x.Role == "" {
		x.Role = RoleNone
	}
	x.HasPassword = x.PasswordHash != ""
	return x
}

func (u *Users) index(username string) int {
	return slices.IndexFunc(u.data.Users, func(x User) bool { return strings.EqualFold(x.Username, username) })
}

func (u *Users) Get(username string) (User, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	i := u.index(username)
	if i < 0 {
		return User{}, false
	}
	return u.view(u.data.Users[i]), true
}

func (u *Users) List() []User {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]User, 0, len(u.data.Users))
	for _, x := range u.data.Users {
		out = append(out, u.view(x))
	}
	return out
}

func (u *Users) Settings() Settings {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.data.Settings
}

func (u *Users) SetSettings(s Settings) (Settings, error) {
	switch s.DefaultRole {
	case RoleNone, RoleViewer, RoleEditor:
	default:
		return Settings{}, badInput("default role must be none, viewer or editor")
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	next := u.data
	next.Settings = s
	if err := u.save(next); err != nil {
		return Settings{}, err
	}
	return s, nil
}

// SignedIn records an oidc sign-in: it creates the account with the default
// role on first sign-in and refreshes name, email and last seen.
func (u *Users) SignedIn(username, name, email string) (User, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	next := u.clone()
	i := u.index(username)
	if i < 0 {
		next.Users = append(next.Users, User{Username: username, Role: u.data.Settings.DefaultRole})
		i = len(next.Users) - 1
	}
	x := &next.Users[i]
	if name != "" {
		x.Name = name
	}
	if email != "" {
		x.Email = email
	}
	x.LastSeen = time.Now().UTC().Format(time.RFC3339)
	if err := u.save(next); err != nil {
		return User{}, err
	}
	return u.view(*x), nil
}

// Touch updates last seen after a local sign-in.
func (u *Users) Touch(username string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	i := u.index(username)
	if i < 0 {
		return
	}
	next := u.clone()
	next.Users[i].LastSeen = time.Now().UTC().Format(time.RFC3339)
	_ = u.save(next)
}

// Update is an admin edit. Empty fields are left unchanged; a non-empty
// password replaces the local password.
type Update struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	Password string `json:"password"`
}

func (u *Users) Create(in Update) (User, error) {
	in.Username = strings.TrimSpace(in.Username)
	if !validUsername.MatchString(in.Username) {
		return User{}, badInput("username may use letters, digits and . _ @ + - (up to 64)")
	}
	if in.Role == "" {
		in.Role = RoleViewer
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.index(in.Username) >= 0 {
		return User{}, badInput("user %q already exists", in.Username)
	}
	next := u.clone()
	next.Users = append(next.Users, User{Username: in.Username})
	x := &next.Users[len(next.Users)-1]
	if err := apply(x, in); err != nil {
		return User{}, err
	}
	if err := u.save(next); err != nil {
		return User{}, err
	}
	return u.view(*x), nil
}

func (u *Users) Update(in Update) (User, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	i := u.index(in.Username)
	if i < 0 {
		return User{}, ErrNotFound
	}
	if in.Role != "" && u.admins[strings.ToLower(u.data.Users[i].Username)] && in.Role != RoleAdmin {
		return User{}, badInput("%s is an admin in the config file", in.Username)
	}
	next := u.clone()
	x := &next.Users[i]
	if err := apply(x, in); err != nil {
		return User{}, err
	}
	if err := u.save(next); err != nil {
		return User{}, err
	}
	return u.view(*x), nil
}

func apply(x *User, in Update) error {
	if in.Role != "" {
		if _, ok := roleRank[in.Role]; !ok {
			return badInput("unknown role %q", in.Role)
		}
		x.Role = in.Role
	}
	if n := strings.TrimSpace(in.Name); n != "" {
		x.Name = n
	}
	if e := strings.TrimSpace(in.Email); e != "" {
		x.Email = e
	}
	if in.Password != "" {
		if len(in.Password) < 8 {
			return badInput("password must be at least 8 characters")
		}
		h, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		x.PasswordHash = string(h)
	}
	return nil
}

func (u *Users) Delete(username string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	i := u.index(username)
	if i < 0 {
		return ErrNotFound
	}
	if u.admins[strings.ToLower(u.data.Users[i].Username)] {
		return badInput("%s is an admin in the config file", username)
	}
	next := u.clone()
	next.Users = slices.Delete(next.Users, i, i+1)
	return u.save(next)
}

// dummyHash makes failed lookups take as long as a real password check.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("parsec-dummy-password"), bcrypt.DefaultCost)

// CheckPassword returns the user if the password matches.
func (u *Users) CheckPassword(username, password string) (User, bool) {
	x, ok := u.Get(username)
	u.mu.Lock()
	hash := dummyHash
	if i := u.index(username); ok && i >= 0 && u.data.Users[i].PasswordHash != "" {
		hash = []byte(u.data.Users[i].PasswordHash)
	} else {
		ok = false
	}
	u.mu.Unlock()
	if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil || !ok {
		return User{}, false
	}
	return x, true
}

func (u *Users) clone() usersFile {
	return usersFile{Settings: u.data.Settings, Users: slices.Clone(u.data.Users)}
}

// save writes atomically and, on success, adopts next.
func (u *Users) save(next usersFile) error {
	b, err := yaml.Marshal(next)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(u.path), ".users-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), u.path); err != nil {
		return err
	}
	u.data = next
	return nil
}
