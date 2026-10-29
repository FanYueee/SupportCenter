ALTER TABLE account_events ADD COLUMN client_ip text NOT NULL DEFAULT '' CHECK (length(client_ip) <= 45);
ALTER TABLE account_events ADD COLUMN user_agent text NOT NULL DEFAULT '' CHECK (length(user_agent) <= 512);
ALTER TABLE tickets ADD COLUMN client_ip text NOT NULL DEFAULT '' CHECK (length(client_ip) <= 45);
ALTER TABLE tickets ADD COLUMN user_agent text NOT NULL DEFAULT '' CHECK (length(user_agent) <= 512);
ALTER TABLE ticket_messages ADD COLUMN client_ip text NOT NULL DEFAULT '' CHECK (length(client_ip) <= 45);
ALTER TABLE ticket_messages ADD COLUMN user_agent text NOT NULL DEFAULT '' CHECK (length(user_agent) <= 512);
ALTER TABLE ticket_events ADD COLUMN client_ip text NOT NULL DEFAULT '' CHECK (length(client_ip) <= 45);
ALTER TABLE ticket_events ADD COLUMN user_agent text NOT NULL DEFAULT '' CHECK (length(user_agent) <= 512);
