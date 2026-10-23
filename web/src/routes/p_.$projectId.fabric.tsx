import { createFileRoute } from '@tanstack/react-router'
import { FabricPage,type FabricSearch } from '../features/fabric/FabricPage'
import { fabricTabs,type FabricTab } from '../features/fabric/NetworkDeviceDrawer'
const text=(v:unknown)=>typeof v==='string'?v.slice(0,128):''
export const Route=createFileRoute('/p_/$projectId/fabric')({validateSearch:(s:Record<string,unknown>):FabricSearch=>({pod:text(s.pod),role:text(s.role),state:text(s.state),device:text(s.device),tab:fabricTabs.includes(s.tab as FabricTab)?s.tab as FabricTab:'basic',abnormal:s.abnormal===true||s.abnormal==='true',descending:s.descending===true||s.descending==='true'}),component:Page})
function Page(){const {projectId}=Route.useParams(),search=Route.useSearch(),navigate=Route.useNavigate();return <FabricPage projectId={projectId} search={search} onSearch={value=>void navigate({search:previous=>({...previous,...value})})}/>}
