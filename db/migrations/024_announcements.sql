CREATE TABLE announcements (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 120),
    body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
    color text NOT NULL CHECK (color IN ('blue', 'green', 'red')),
    enabled boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by bigint NOT NULL REFERENCES users(id),
    updated_by bigint NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX announcements_visible_idx ON announcements(id DESC) WHERE enabled;
