-- WP-3.3 nettest tickets (FR-11.3 / FR-4.7): the connectivity module adds
-- the T6 configuration class (ip_conflict / vlan_unreachable / pfc_misconfig
-- / mtu_mismatch) and the 'nettest' issue source to the taxonomy.
-- Rollback: restore the previous lists:
--   ALTER TABLE issues DROP CONSTRAINT issues_category_check;
--   ALTER TABLE issues ADD CONSTRAINT issues_category_check CHECK(category IN('T1','T2','T3','T4','T5','T7','config','other'));
--   ALTER TABLE issues DROP CONSTRAINT issues_source_check;
--   ALTER TABLE issues ADD CONSTRAINT issues_source_check CHECK(source IN('topology','g2','g5','g6','portmon','bmc'));
ALTER TABLE issues DROP CONSTRAINT issues_category_check;
ALTER TABLE issues ADD CONSTRAINT issues_category_check CHECK(category IN('T1','T2','T3','T4','T5','T6','T7','config','other'));
ALTER TABLE issues DROP CONSTRAINT issues_source_check;
ALTER TABLE issues ADD CONSTRAINT issues_source_check CHECK(source IN('topology','g2','g5','g6','portmon','bmc','nettest'));
