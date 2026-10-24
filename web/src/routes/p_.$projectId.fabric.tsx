import { createFileRoute } from '@tanstack/react-router'
import { FabricPage,type FabricSearch } from '../features/fabric/FabricPage'
import { fabricTabs,type FabricTab } from '../features/fabric/NetworkDeviceDrawer'
const text=(v:unknown)=>typeof v==='string'?v.slice(0,128):''
export const Route=createFileRoute('/p_/$projectId/fabric')({validateSearch:(s:Record<string,unknown>):FabricSearch=>({trendPort:text(s.trendPort),trendHours:s.trendHours===48||s.trendHours==='48'?48:24,trendMetrics:typeof s.trendMetrics==='string'?s.trendMetrics.split(',').filter(v=>['rx_dbm','tx_dbm','temperature_c','voltage_v','bias_ma'].includes(v)).join(','):'rx_dbm,tx_dbm',pod:text(s.pod),role:text(s.role),state:text(s.state),device:text(s.device),tab:fabricTabs.includes(s.tab as FabricTab)?s.tab as FabricTab:'basic',abnormal:s.abnormal===true||s.abnormal==='true',descending:s.descending===true||s.descending==='true'}),component:Page})
function Page(){const {projectId}=Route.useParams(),search=Route.useSearch(),navigate=Route.useNavigate();return <FabricPage projectId={projectId} search={search} onSearch={value=>void navigate({search:previous=>({...previous,...value})})}/>}
