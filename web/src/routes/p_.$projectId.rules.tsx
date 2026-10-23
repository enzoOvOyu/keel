import {createFileRoute} from '@tanstack/react-router'
import {NetworkRulesPage} from '../features/network/NetworkRulesPage'
export const Route=createFileRoute('/p_/$projectId/rules')({component:Page})
function Page(){return <NetworkRulesPage projectId={Route.useParams().projectId}/>}
