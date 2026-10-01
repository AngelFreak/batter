package vpn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound means no profile has the given id.
var ErrNotFound = errors.New("vpn profile not found")

// applyTimeout bounds applying or removing one profile's tunnel.
const applyTimeout = 30 * time.Second

// Service owns the VPN profiles: their rows in the database, and for each
// one a slot's kill switch and tunnel. A tunnel never comes up before its
// slot's kill switch is installed.
type Service struct {
	DB     *pgxpool.Pool
	Run    RunFunc // nil = Exec
	Logger *slog.Logger
	// CheckExitAs reports the public IP that uid's traffic leaves from; nil
	// if unavailable.
	CheckExitAs func(ctx context.Context, uid uint32) (string, error)

	mu   sync.Mutex
	errs map[Slot]error // last apply error per slot
}

// Assignment is where a phone's traffic goes: its profile's routing table,
// and the profile's DNS server (invalid if it has no IPv4 one).
type Assignment struct {
	Table int
	DNS   netip.Addr
}

// ProfileInfo describes a profile for admins. It never contains secrets.
type ProfileInfo struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Enabled    bool           `json:"enabled"`
	Devices    int            `json:"devices"` // phones assigned to it
	Config     RedactedConfig `json:"config"`
	Status     *Status        `json:"status,omitempty"`
	ApplyError string         `json:"apply_error,omitempty"`
}

// ProfileName is what non-admins may see of a profile.
type ProfileName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ProfileUpdate holds the fields to change; nil fields are kept.
type ProfileUpdate struct {
	Name    *string
	Config  *string
	Enabled *bool
}

type profile struct {
	id, name string
	slot     Slot
	config   string
	enabled  bool
	cfg      *Config
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Sync applies every stored profile; call it at startup. Per-profile
// failures are recorded (see ProfileInfo.ApplyError), not returned.
func (s *Service) Sync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	profiles, err := s.load(ctx, s.DB, "")
	if err != nil {
		return err
	}
	for _, p := range profiles {
		s.apply(ctx, p)
	}
	return nil
}

// Create stores and applies a new profile. Invalid input returns a
// *ConfigError; a failure to apply is reported in ApplyError.
func (s *Service) Create(ctx context.Context, name, config string, enabled bool) (ProfileInfo, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ProfileInfo{}, &ConfigError{"name is required"}
	}
	cfg, err := Parse(config)
	if err != nil {
		return ProfileInfo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	slot, err := freeSlot(ctx, s.DB)
	if err != nil {
		return ProfileInfo{}, err
	}
	p := &profile{name: name, slot: slot, config: config, enabled: enabled, cfg: cfg}
	err = s.DB.QueryRow(ctx,
		"INSERT INTO vpn_profiles (name, slot, config, enabled) VALUES ($1, $2, $3, $4) RETURNING id::text",
		name, int(slot), config, enabled,
	).Scan(&p.id)
	if err != nil {
		return ProfileInfo{}, nameTaken(err, name)
	}
	s.Logger.Info("vpn profile created", "profile", name, "slot", slot)
	s.applyDetached(ctx, p)
	return s.info(ctx, p)
}

// Update changes a profile and re-applies it if its config or enabled flag
// changed.
func (s *Service) Update(ctx context.Context, id string, u ProfileUpdate) (ProfileInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.get(ctx, id)
	if err != nil {
		return ProfileInfo{}, err
	}
	reapply := false
	if u.Name != nil {
		if p.name = strings.TrimSpace(*u.Name); p.name == "" {
			return ProfileInfo{}, &ConfigError{"name is required"}
		}
	}
	if u.Config != nil && *u.Config != p.config {
		if p.cfg, err = Parse(*u.Config); err != nil {
			return ProfileInfo{}, err
		}
		p.config, reapply = *u.Config, true
	}
	if u.Enabled != nil && *u.Enabled != p.enabled {
		p.enabled, reapply = *u.Enabled, true
	}
	_, err = s.DB.Exec(ctx,
		"UPDATE vpn_profiles SET name = $1, config = $2, enabled = $3, updated_at = now() WHERE id = $4",
		p.name, p.config, p.enabled, p.id,
	)
	if err != nil {
		return ProfileInfo{}, nameTaken(err, p.name)
	}
	if reapply {
		s.applyDetached(ctx, p)
	}
	return s.info(ctx, p)
}

// Delete removes a profile, its tunnel and routing. It returns the phones
// that were assigned to it; they are unassigned (ON DELETE SET NULL), which
// leaves them without internet.
func (s *Service) Delete(ctx context.Context, id string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	rows, err := tx.Query(ctx, "SELECT serial FROM devices WHERE vpn_profile_id = $1 ORDER BY serial", p.id)
	if err != nil {
		return nil, err
	}
	serials, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM vpn_profiles WHERE id = $1", p.id); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), applyTimeout)
	defer cancel()
	// Until the LAN drops the phones' routes, their packets find nothing in
	// the flushed table and fall to the LAN's catch-all.
	s.tunnels().teardown(ctx, p.slot)
	delete(s.errs, p.slot)
	s.Logger.Info("vpn profile deleted", "profile", p.name, "slot", p.slot, "unassigned", serials)
	return serials, nil
}

// List returns every profile with its status.
func (s *Service) List(ctx context.Context) ([]ProfileInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	profiles, err := s.load(ctx, s.DB, "")
	if err != nil {
		return nil, err
	}
	out := []ProfileInfo{}
	for _, p := range profiles {
		info, err := s.info(ctx, p)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

// Get returns one profile with its status.
func (s *Service) Get(ctx context.Context, id string) (ProfileInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.get(ctx, id)
	if err != nil {
		return ProfileInfo{}, err
	}
	return s.info(ctx, p)
}

// Names lists profile ids and names, for anyone choosing a phone's profile.
func (s *Service) Names(ctx context.Context) ([]ProfileName, error) {
	rows, err := s.DB.Query(ctx, "SELECT id::text, name FROM vpn_profiles ORDER BY name")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ProfileName, error) {
		var n ProfileName
		err := row.Scan(&n.ID, &n.Name)
		return n, err
	})
}

// Exists reports ErrNotFound unless a profile has the given id.
func (s *Service) Exists(ctx context.Context, id string) error {
	_, err := s.get(ctx, id)
	return err
}

// Assignments maps each phone with a profile to where its traffic goes.
// A disabled profile's phones are still routed to its table, whose kill
// switch blocks them.
func (s *Service) Assignments(ctx context.Context) (map[string]Assignment, error) {
	rows, err := s.DB.Query(ctx,
		`SELECT d.serial, p.slot, p.config FROM devices d JOIN vpn_profiles p ON p.id = d.vpn_profile_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Assignment{}
	for rows.Next() {
		var serial, config string
		var slot int
		if err := rows.Scan(&serial, &slot, &config); err != nil {
			return nil, err
		}
		a := Assignment{Table: Slot(slot).Table()}
		if cfg, err := Parse(config); err == nil {
			for _, d := range cfg.DNS {
				if ip, err := netip.ParseAddr(d); err == nil && ip.Is4() {
					a.DNS = ip
					break
				}
			}
		}
		out[serial] = a
	}
	return out, rows.Err()
}

// CheckExit reports the public IP the profile's phones exit from, by making
// a request as its checker uid, which is routed like the phones.
func (s *Service) CheckExit(ctx context.Context, id string) (string, error) {
	p, err := s.get(ctx, id)
	if err != nil {
		return "", err
	}
	if s.CheckExitAs == nil {
		return "", errors.New("exit IP check is unavailable on this server")
	}
	return s.CheckExitAs(ctx, p.slot.UID())
}

// ImportLegacy migrates the single-tunnel version's config at path, if
// present, into a profile named "Default", assigns the phones that version
// tethered to it, and removes the file. Without a file those phones stay
// off: tethering no longer has a direct option.
func (s *Service) ImportLegacy(ctx context.Context, path string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		rows, err := s.DB.Query(ctx, "DELETE FROM legacy_tethered_devices RETURNING serial")
		if err != nil {
			return err
		}
		serials, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		if len(serials) > 0 {
			s.Logger.Warn("tethering turned off: it now needs a VPN profile", "serials", serials)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read legacy wireguard config: %w", err)
	}
	var legacy struct {
		Enabled bool   `json:"enabled"`
		Config  string `json:"config"`
	}
	if err := json.Unmarshal(b, &legacy); err != nil {
		return fmt.Errorf("legacy wireguard config %s: %w", path, err)
	}
	if _, err := Parse(legacy.Config); err != nil {
		return fmt.Errorf("legacy wireguard config %s: %w", path, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	slot, err := freeSlot(ctx, tx)
	if err != nil {
		return err
	}
	var id string
	err = tx.QueryRow(ctx,
		"INSERT INTO vpn_profiles (name, slot, config, enabled) VALUES ('Default', $1, $2, $3) RETURNING id::text",
		int(slot), legacy.Config, legacy.Enabled,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("import legacy wireguard config: %w", nameTaken(err, "Default"))
	}
	tag, err := tx.Exec(ctx,
		"UPDATE devices SET vpn_profile_id = $1 WHERE serial IN (SELECT serial FROM legacy_tethered_devices)", id)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM legacy_tethered_devices"); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("imported legacy wireguard config but couldn't remove it: %w", err)
	}
	s.Logger.Info("imported legacy wireguard config as profile Default", "phones", tag.RowsAffected())
	return nil
}

// applyDetached applies p even if the caller goes away: a half-applied
// config is worse than a slow response.
func (s *Service) applyDetached(ctx context.Context, p *profile) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), applyTimeout)
	defer cancel()
	s.apply(ctx, p)
}

// apply makes p's slot match it: kill switch always; tunnel only while
// enabled, and only once the kill switch is in. While the tunnel is down
// the kill switch blocks the slot's phones.
func (s *Service) apply(ctx context.Context, p *profile) {
	if s.errs == nil {
		s.errs = map[Slot]error{}
	}
	t := s.tunnels()
	err := t.installFirewall(ctx)
	if err == nil {
		err = t.installKillSwitch(ctx, p.slot)
	}
	if err != nil {
		t.takeDown(ctx, p.slot)
		s.errs[p.slot] = err
		s.Logger.Error("vpn: kill switch not installed; tunnel kept down", "profile", p.name, "error", err)
		return
	}
	if !p.enabled {
		t.takeDown(ctx, p.slot)
		s.errs[p.slot] = nil
		return
	}
	err = t.bringUp(ctx, p.slot, p.cfg)
	s.errs[p.slot] = err
	if err != nil {
		s.Logger.Error("vpn: tunnel not up; its phones have no internet until it is", "profile", p.name, "error", err)
	} else {
		s.Logger.Info("vpn: tunnel up", "profile", p.name, "iface", p.slot.iface())
	}
}

func (s *Service) tunnels() *tunnels {
	return &tunnels{run: s.Run, logger: s.Logger}
}

func (s *Service) info(ctx context.Context, p *profile) (ProfileInfo, error) {
	info := ProfileInfo{ID: p.id, Name: p.name, Enabled: p.enabled, Config: p.cfg.Redacted()}
	if err := s.DB.QueryRow(ctx, "SELECT count(*) FROM devices WHERE vpn_profile_id = $1", p.id).Scan(&info.Devices); err != nil {
		return ProfileInfo{}, err
	}
	if err := s.errs[p.slot]; err != nil {
		info.ApplyError = err.Error()
	}
	if p.enabled {
		info.Status = s.tunnels().status(ctx, p.slot)
	}
	return info, nil
}

func (s *Service) get(ctx context.Context, id string) (*profile, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrNotFound
	}
	profiles, err := s.load(ctx, s.DB, id)
	if err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, ErrNotFound
	}
	return profiles[0], nil
}

// load reads all profiles, or the one with id.
func (s *Service) load(ctx context.Context, q querier, id string) ([]*profile, error) {
	sql := "SELECT id::text, name, slot, config, enabled FROM vpn_profiles"
	var args []any
	if id != "" {
		sql += " WHERE id = $1"
		args = append(args, id)
	}
	rows, err := q.Query(ctx, sql+" ORDER BY name", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*profile
	for rows.Next() {
		p := &profile{}
		var slot int
		if err := rows.Scan(&p.id, &p.name, &slot, &p.config, &p.enabled); err != nil {
			return nil, err
		}
		p.slot = Slot(slot)
		if p.cfg, err = Parse(p.config); err != nil {
			return nil, fmt.Errorf("stored vpn profile %q: %w", p.name, err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// freeSlot returns the lowest unused slot.
func freeSlot(ctx context.Context, q querier) (Slot, error) {
	rows, err := q.Query(ctx, "SELECT slot FROM vpn_profiles")
	if err != nil {
		return 0, err
	}
	used, err := pgx.CollectRows(rows, pgx.RowTo[int16])
	if err != nil {
		return 0, err
	}
	taken := map[int16]bool{}
	for _, u := range used {
		taken[u] = true
	}
	for i := range int16(MaxProfiles) {
		if !taken[i] {
			return Slot(i), nil
		}
	}
	return 0, &ConfigError{fmt.Sprintf("at most %d VPN profiles are supported", MaxProfiles)}
}

// nameTaken turns a unique violation on the name into a ConfigError.
func nameTaken(err error, name string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "name") {
		return &ConfigError{fmt.Sprintf("a profile named %q already exists", name)}
	}
	return err
}
