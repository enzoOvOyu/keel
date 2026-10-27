package platform

import "context"

func (s *Store) GetProjectAsset(ctx context.Context, a *AuthSession, project, asset string) (*ProjectAssetSummary, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	var item ProjectAssetSummary
	e = tx.QueryRowContext(ctx, projectAssetSummarySelect+` WHERE pa.project_id=$1 AND pa.id=$2`, project, asset).Scan(&item.ID, &item.MachineID, &item.SerialNumber, &item.Hostname, &item.IpReference, &item.State, &item.Version, &item.ModelName, &item.Rack, &item.UnitPosition, &item.SshTrust, &item.SshTrustSource, &item.ModelID, &item.ModelVersionID, &item.ModelVersion)
	if e != nil {
		return nil, e
	}
	return &item, tx.Commit()
}
