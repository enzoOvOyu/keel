import {createFileRoute} from '@tanstack/react-router'
import {NetworkDeliveryPage,type DeliverySearch} from '../features/network/NetworkDeliveryPage'
export const Route=createFileRoute('/p_/$projectId/delivery')({validateSearch:(value:Record<string,unknown>):DeliverySearch=>({tab:value.tab==='references'||value.tab==='history'?value.tab:'preflight',kind:value.kind==='delivery'?'delivery':'draft',page:Math.max(1,Math.min(1000000,Math.trunc(Number(value.page)||1)))}),component:Page})
function Page(){const {projectId}=Route.useParams(),search=Route.useSearch(),navigate=Route.useNavigate();return <NetworkDeliveryPage projectId={projectId} search={search} onSearch={value=>void navigate({search:{...search,...value},replace:true})}/>}
