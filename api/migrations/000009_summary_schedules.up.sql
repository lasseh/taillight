-- Summary schedules: periodic log digest reports sent to notification channels.

CREATE TABLE summary_schedules (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    enabled       BOOLEAN NOT NULL DEFAULT true,
    frequency     TEXT NOT NULL CHECK (frequency IN ('daily', 'weekly', 'monthly')),
    day_of_week   INT CHECK (day_of_week BETWEEN 0 AND 6),
    day_of_month  INT CHECK (day_of_month BETWEEN 1 AND 28),
    time_of_day   TIME NOT NULL DEFAULT '07:00',
    timezone      TEXT NOT NULL DEFAULT 'UTC',
    event_kinds   TEXT[] NOT NULL DEFAULT '{srvlog}',
    severity_max  INT,
    hostname      TEXT NOT NULL DEFAULT '',
    top_n         INT NOT NULL DEFAULT 25,
    last_run_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE summary_schedule_channels (
    schedule_id BIGINT REFERENCES summary_schedules(id) ON DELETE CASCADE,
    channel_id  BIGINT REFERENCES notification_channels(id) ON DELETE CASCADE,
    PRIMARY KEY (schedule_id, channel_id)
);
