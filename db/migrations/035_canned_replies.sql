-- Ready-made replies staff insert into a message; admins keep the list.
CREATE TABLE canned_replies (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 80),
 body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 4000),
 version bigint NOT NULL DEFAULT 1,
 created_by bigint REFERENCES users(id) ON DELETE SET NULL,
 updated_by bigint REFERENCES users(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
