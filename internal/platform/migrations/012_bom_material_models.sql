-- FR-2.1/A.1 model references cover serialized components and optics as
-- well as servers and switches. Keep published version IDs on the P1 asset
-- projection so the BOM binding cannot silently follow a newer version.
ALTER TABLE device_models DROP CONSTRAINT device_models_kind_check;
ALTER TABLE device_models ADD CONSTRAINT device_models_kind_check CHECK(kind IN('rack','server','compute-tray','switch','nic','gpu','psu','bmc','optic_module'));
ALTER TABLE project_assets ADD COLUMN model_version_id varchar(32) REFERENCES device_model_versions(id);
ALTER TABLE physical_ports ADD COLUMN rail_label varchar(40);
ALTER TABLE bom_import_packages ADD COLUMN network_baseline_ref varchar(120);
ALTER TABLE bom_import_packages ADD COLUMN credential_batch_ref varchar(120);
