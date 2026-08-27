CREATE TABLE IF NOT EXISTS clicks (
    click_date DATE NOT NULL,
    author_id  BIGINT NOT NULL CHECK (author_id > 0),
    user_id    BIGINT NOT NULL CHECK (user_id > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (click_date, author_id, user_id)
);

COMMENT ON TABLE clicks IS 'One unique user click per author and UTC day';
