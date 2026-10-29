package database

import (
	"context"
	"strings"
	"unicode/utf8"

	"supportcenter/internal/model"
)

func (s *Store) AIPrompts(ctx context.Context) (p model.AIPrompts, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT general_prompt,billing_prompt,technical_prompt FROM ai_prompt_settings WHERE id=1`).Scan(&p.General, &p.Billing, &p.Technical)
	return
}

func validAIPrompt(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) && len([]rune(value)) <= 4000
}

func (s *Store) SaveAIPrompts(ctx context.Context, p model.AIPrompts) error {
	if !validAIPrompt(p.General) || !validAIPrompt(p.Billing) || !validAIPrompt(p.Technical) {
		return ErrInvalid
	}
	_, err := s.Pool.Exec(ctx, `UPDATE ai_prompt_settings SET general_prompt=$1,billing_prompt=$2,technical_prompt=$3,updated_at=now() WHERE id=1`, p.General, p.Billing, p.Technical)
	return err
}
