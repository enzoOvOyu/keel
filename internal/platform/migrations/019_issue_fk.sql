-- WP-2.13: issues.difference_id referenced lldp_differences only, but T7
-- topology issues reference pcie_diffs rows. The reference is now semantic
-- (validated at creation inside the same transaction): drop the single-table
-- FK so both evidence kinds can bind. Rollback: re-add the FK.
ALTER TABLE issues DROP CONSTRAINT issues_difference_id_fkey;
