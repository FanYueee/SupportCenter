CREATE TABLE ai_prompt_settings (
 id smallint PRIMARY KEY DEFAULT 1 CHECK (id=1),
 general_prompt text NOT NULL DEFAULT '' CHECK (char_length(general_prompt) <= 4000),
 billing_prompt text NOT NULL DEFAULT '' CHECK (char_length(billing_prompt) <= 4000),
 technical_prompt text NOT NULL DEFAULT '' CHECK (char_length(technical_prompt) <= 4000),
 updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO ai_prompt_settings(id) VALUES(1);
