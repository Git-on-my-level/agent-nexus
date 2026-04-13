package secrets

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type SecretMetadata struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	CreatedBy   string  `json:"created_by"`
	UpdatedBy   string  `json:"updated_by"`
}

type Store struct {
	db        *sql.DB
	encryptor *Encryptor
}

func NewStore(db *sql.DB, encryptor *Encryptor) *Store {
	return &Store{db: db, encryptor: encryptor}
}

func (s *Store) HasEncryptor() bool {
	return s.encryptor != nil
}

type CreateSecretInput struct {
	Name        string
	Value       string
	Description string
	ActorID     string
}

func (s *Store) Create(ctx context.Context, input CreateSecretInput) (SecretMetadata, error) {
	if s.encryptor == nil {
		return SecretMetadata{}, fmt.Errorf("secrets encryption is not configured (set OAR_SECRETS_KEY)")
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return SecretMetadata{}, fmt.Errorf("secret name is required")
	}
	value := input.Value
	if value == "" {
		return SecretMetadata{}, fmt.Errorf("secret value is required")
	}

	ciphertext, nonce, err := s.encryptor.Encrypt([]byte(value))
	if err != nil {
		return SecretMetadata{}, fmt.Errorf("encrypt secret: %w", err)
	}

	id := "sec_" + uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	var desc *string
	if d := strings.TrimSpace(input.Description); d != "" {
		desc = &d
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO secrets (id, name, description, ciphertext, nonce, key_id, actor_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, desc, ciphertext, nonce, s.encryptor.KeyID(), input.ActorID, now, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return SecretMetadata{}, fmt.Errorf("secret with name %q already exists", name)
		}
		return SecretMetadata{}, fmt.Errorf("insert secret: %w", err)
	}

	return SecretMetadata{
		ID:          id,
		Name:        name,
		Description: desc,
		CreatedAt:   now,
		UpdatedAt:   now,
		CreatedBy:   input.ActorID,
		UpdatedBy:   input.ActorID,
	}, nil
}

func (s *Store) List(ctx context.Context) ([]SecretMetadata, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, created_at, updated_at, actor_id, updated_by FROM secrets ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list secrets: %w", err)
	}
	defer rows.Close()

	var result []SecretMetadata
	for rows.Next() {
		var m SecretMetadata
		var desc, updatedBy sql.NullString
		if err := rows.Scan(&m.ID, &m.Name, &desc, &m.CreatedAt, &m.UpdatedAt, &m.CreatedBy, &updatedBy); err != nil {
			return nil, fmt.Errorf("scan secret: %w", err)
		}
		if desc.Valid {
			m.Description = &desc.String
		}
		if updatedBy.Valid {
			m.UpdatedBy = updatedBy.String
		} else {
			m.UpdatedBy = m.CreatedBy
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func (s *Store) GetByID(ctx context.Context, id string) (SecretMetadata, error) {
	var m SecretMetadata
	var desc, updatedBy sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, description, created_at, updated_at, actor_id, updated_by FROM secrets WHERE id = ?`,
		id,
	).Scan(&m.ID, &m.Name, &desc, &m.CreatedAt, &m.UpdatedAt, &m.CreatedBy, &updatedBy)
	if err == sql.ErrNoRows {
		return SecretMetadata{}, fmt.Errorf("secret not found")
	}
	if err != nil {
		return SecretMetadata{}, fmt.Errorf("get secret: %w", err)
	}
	if desc.Valid {
		m.Description = &desc.String
	}
	if updatedBy.Valid {
		m.UpdatedBy = updatedBy.String
	} else {
		m.UpdatedBy = m.CreatedBy
	}
	return m, nil
}

func (s *Store) Reveal(ctx context.Context, id string) (string, string, error) {
	if s.encryptor == nil {
		return "", "", fmt.Errorf("secrets encryption is not configured (set OAR_SECRETS_KEY)")
	}

	var name string
	var ciphertext, nonce []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT name, ciphertext, nonce FROM secrets WHERE id = ?`,
		id,
	).Scan(&name, &ciphertext, &nonce)
	if err == sql.ErrNoRows {
		return "", "", fmt.Errorf("secret not found")
	}
	if err != nil {
		return "", "", fmt.Errorf("get secret for reveal: %w", err)
	}

	plaintext, err := s.encryptor.Decrypt(ciphertext, nonce)
	if err != nil {
		return "", "", fmt.Errorf("decrypt secret: %w", err)
	}

	return name, string(plaintext), nil
}

func (s *Store) RevealBatchByNames(ctx context.Context, names []string) ([]struct{ Name, Value string }, error) {
	if s.encryptor == nil {
		return nil, fmt.Errorf("secrets encryption is not configured (set OAR_SECRETS_KEY)")
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("at least one secret name is required")
	}

	placeholders := make([]string, len(names))
	queryArgs := make([]any, len(names))
	for i, n := range names {
		placeholders[i] = "?"
		queryArgs[i] = strings.TrimSpace(n)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT name, ciphertext, nonce FROM secrets WHERE name IN (`+strings.Join(placeholders, ",")+`)`,
		queryArgs...,
	)
	if err != nil {
		return nil, fmt.Errorf("query secrets batch: %w", err)
	}
	defer rows.Close()

	found := map[string]struct{ Name, Value string }{}
	for rows.Next() {
		var name string
		var ciphertext, nonce []byte
		if err := rows.Scan(&name, &ciphertext, &nonce); err != nil {
			return nil, fmt.Errorf("scan secret batch: %w", err)
		}
		plaintext, err := s.encryptor.Decrypt(ciphertext, nonce)
		if err != nil {
			return nil, fmt.Errorf("decrypt secret %q: %w", name, err)
		}
		found[name] = struct{ Name, Value string }{Name: name, Value: string(plaintext)}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Verify all requested names were found
	for _, n := range names {
		trimmed := strings.TrimSpace(n)
		if _, ok := found[trimmed]; !ok {
			return nil, fmt.Errorf("secret %q not found", trimmed)
		}
	}

	// Return in request order
	result := make([]struct{ Name, Value string }, len(names))
	for i, n := range names {
		result[i] = found[strings.TrimSpace(n)]
	}
	return result, nil
}

func (s *Store) Update(ctx context.Context, id string, value string, description *string, actorID string) (SecretMetadata, error) {
	if s.encryptor == nil {
		return SecretMetadata{}, fmt.Errorf("secrets encryption is not configured (set OAR_SECRETS_KEY)")
	}
	if strings.TrimSpace(value) == "" {
		return SecretMetadata{}, fmt.Errorf("secret value is required")
	}

	ciphertext, nonce, err := s.encryptor.Encrypt([]byte(value))
	if err != nil {
		return SecretMetadata{}, fmt.Errorf("encrypt secret: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	var result sql.Result
	if description != nil {
		result, err = s.db.ExecContext(ctx,
			`UPDATE secrets SET ciphertext = ?, nonce = ?, key_id = ?, description = ?, updated_by = ?, updated_at = ? WHERE id = ?`,
			ciphertext, nonce, s.encryptor.KeyID(), *description, actorID, now, id,
		)
	} else {
		result, err = s.db.ExecContext(ctx,
			`UPDATE secrets SET ciphertext = ?, nonce = ?, key_id = ?, updated_by = ?, updated_at = ? WHERE id = ?`,
			ciphertext, nonce, s.encryptor.KeyID(), actorID, now, id,
		)
	}
	if err != nil {
		return SecretMetadata{}, fmt.Errorf("update secret: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return SecretMetadata{}, fmt.Errorf("secret not found")
	}

	return s.GetByID(ctx, id)
}

func (s *Store) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM secrets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete secret: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("secret not found")
	}
	return nil
}

func (s *Store) HasSecrets(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM secrets`).Scan(&count)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return false, nil
		}
		return false, err
	}
	return count > 0, nil
}
