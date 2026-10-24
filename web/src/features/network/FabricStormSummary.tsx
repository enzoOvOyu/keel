import {useTranslation} from 'react-i18next'
import type {components} from '../../generated/api'

type Props = {
  storm: components['schemas']['FabricStorm']
  devices: components['schemas']['NetworkDeviceHealth'][]
  onSelect: (device: string) => void
}

export function FabricStormSummary({storm, devices, onSelect}: Props) {
  const {t} = useTranslation()
  const labels = new Map(devices.map(({device}) => [device.id, device.hostname || device.serial]))
  return <div className="network-storm" data-state={storm.state}>
    <p role={storm.state === 'escalated' ? 'alert' : 'status'}>
      <strong>{t('networkStormTitle')}: {t('networkStorm_' + storm.state)}</strong>
      {' · '}{t('networkStormPorts', {count: storm.affected_ports, threshold: storm.threshold_ports})}
      {storm.incomplete && <> · {t('networkHealthIncomplete')}</>}
      {storm.approximation && <> · {t('networkStormApproximation')}</>}
    </p>
    {storm.affected_device_ids.length > 0 && <details>
      <summary>{t('networkStormDevices', {count: storm.affected_device_ids.length})}</summary>
      <ul>{storm.affected_device_ids.map(id => <li key={id}>
        <button className="fabric-device-link" onClick={() => onSelect(id)}>{labels.get(id) || id}</button>
      </li>)}</ul>
      <p>{t('fabricRule')}: {storm.applied_rule_ids.join(', ')}</p>
    </details>}
  </div>
}
