-- Items from an outside source (the Portal's invoices, services, …) tagged on
-- a ticket, kept as they were when tagged so tickets show them without asking
-- the source again. A tag that only staff notes carry stays internal.
CREATE TABLE ticket_references (
 ticket_id bigint NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_]{0,23}$'),
 ref_id text NOT NULL CHECK (ref_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$'),
 trigger text NOT NULL CHECK (trigger IN ('$','!','&','%','@','^')),
 kind_label text NOT NULL CHECK (char_length(kind_label) BETWEEN 1 AND 20),
 kind_label_en text NOT NULL DEFAULT '' CHECK (char_length(kind_label_en) <= 20),
 title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 80),
 detail text NOT NULL DEFAULT '' CHECK (char_length(detail) <= 120),
 customer_url text NOT NULL CHECK (char_length(customer_url) BETWEEN 9 AND 2048),
 staff_url text NOT NULL CHECK (char_length(staff_url) BETWEEN 9 AND 2048),
 internal boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (ticket_id, kind, ref_id)
);
