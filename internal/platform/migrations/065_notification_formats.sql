-- Policy identity is still immutable and hashes provider/recipient/credential
-- bindings. Historical CloudEvents sinks retain their original policy hash.
ALTER TABLE notification_webhook_policies
 ADD COLUMN delivery_format varchar(16) NOT NULL DEFAULT 'cloudevents'
 CHECK(delivery_format IN ('cloudevents','resend','slack'));

ALTER TABLE notification_webhook_deliveries
 DROP CONSTRAINT notification_webhook_deliveries_last_error_check;
ALTER TABLE notification_webhook_deliveries
 ADD CONSTRAINT notification_webhook_deliveries_last_error_check
 CHECK(last_error IN ('transport_unknown','http_rejected','attempts_exhausted','event_expired'));
