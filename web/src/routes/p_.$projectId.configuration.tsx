import {createFileRoute} from '@tanstack/react-router'
import {NetworkConfiguration} from '../features/network/NetworkConfiguration'
export const Route=createFileRoute('/p_/$projectId/configuration')({component:Page})
function Page(){const {projectId}=Route.useParams();return <NetworkConfiguration projectId={projectId}/>}
