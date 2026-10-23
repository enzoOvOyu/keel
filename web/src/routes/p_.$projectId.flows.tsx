import {createFileRoute} from '@tanstack/react-router'
import {FlowsPage} from '../features/flows/FlowsPage'
import {flowSearch} from '../lib/flow-workbench'
export const Route=createFileRoute('/p_/$projectId/flows')({validateSearch:flowSearch,component:Page})
function Page(){const{projectId}=Route.useParams(),search=Route.useSearch(),navigate=Route.useNavigate();return <FlowsPage projectId={projectId} search={search} onSearch={value=>void navigate({search:{...search,...value},replace:true})}/>}
