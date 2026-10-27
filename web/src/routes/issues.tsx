import { createFileRoute } from '@tanstack/react-router'
import { IssuesPage } from '../features/issues/IssuesPage'
import { issueSearch } from '../lib/issues'
export const Route = createFileRoute('/issues')({validateSearch:issueSearch,component:IssuesPage})
