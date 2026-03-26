-- Remove 'ntfy' from the notification_channels type check constraint
-- NOTE: this will fail if any rows with type='ntfy' exist
ALTER TABLE notification_channels
    DROP CONSTRAINT notification_channels_type_check,
    ADD CONSTRAINT notification_channels_type_check
        CHECK (type IN ('slack', 'webhook', 'email'));
