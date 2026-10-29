CREATE TABLE automation_settings (
 id smallint PRIMARY KEY DEFAULT 1 CHECK (id=1),
 timezone text NOT NULL DEFAULT 'Asia/Taipei',
 weekdays smallint NOT NULL DEFAULT 31 CHECK (weekdays BETWEEN 0 AND 127),
 office_start text NOT NULL DEFAULT '09:00',
 office_end text NOT NULL DEFAULT '18:00',
 on_create_enabled boolean NOT NULL DEFAULT false,
 on_create_body text NOT NULL DEFAULT '',
 off_hours_enabled boolean NOT NULL DEFAULT false,
 off_hours_body text NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO automation_settings(id) VALUES(1);
