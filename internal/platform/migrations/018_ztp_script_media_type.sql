-- WP-1.17: allow the Cumulus isolation ZTP script to be registered and
-- independently approved in the media lifecycle before boot service delivery.
ALTER TABLE media_items DROP CONSTRAINT media_items_type_check;
ALTER TABLE media_items ADD CONSTRAINT media_items_type_check
 CHECK(type IN('driver','firmware','os','nccl','tool','config','ztp_script'));
