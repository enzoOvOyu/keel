-- Device script delivery is observable under the same append-only evidence model.
ALTER TABLE ztp_device_observations DROP CONSTRAINT ztp_device_observations_boot_channel_check;
ALTER TABLE ztp_device_observations ADD CONSTRAINT ztp_device_observations_boot_channel_check CHECK (boot_channel IN ('143','239'));
ALTER TABLE ztp_device_observations ADD CONSTRAINT ztp_device_observations_239_delivery_only CHECK (boot_channel <> '239' OR event_type = 'data-served');
