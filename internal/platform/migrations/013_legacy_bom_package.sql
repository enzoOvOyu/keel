-- A.8.3 server-only wide-sheet compatibility shares the standard batch/hash
-- namespace and revision chain, but cannot imply fabric material readiness.
ALTER TABLE bom_import_packages DROP CONSTRAINT bom_import_packages_schema_version_check;
ALTER TABLE bom_import_packages ADD CONSTRAINT bom_import_packages_schema_version_check CHECK(schema_version IN('bom-import/v1','legacy-wide/v1'));
ALTER TABLE bom_import_packages ALTER COLUMN profile_version_ref DROP NOT NULL;
ALTER TABLE bom_import_packages ADD CONSTRAINT bom_import_packages_profile_required CHECK(schema_version<>'bom-import/v1' OR profile_version_ref IS NOT NULL);
