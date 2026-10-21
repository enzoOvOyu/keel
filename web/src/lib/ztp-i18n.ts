export const ztpTranslations = {
  zh: {
    ztpTab: 'ZTP 仿真', ztpTitle: 'ZTP 引导仿真', ztpBoundary: '这里仅模拟引导时间线。仿真完成不代表交换机已开通，也不产生网络就绪证明。',
    ztpIsolationManifest: '隔离脚本待审内容', ztpIsolationApproval: '在介质库登记 ztp_script，填入本页 SHA256/大小并由另一位审批人批准；Kea 239 URL 的 policy 使用已批准介质 ID。', ztpIsolationReview: '查看脚本',
    ztpKeaTitle: 'mock Kea 管理网选项', ztpKeaBoundary: '仅供仿真；staging 后需按哈希 commit，真实 Kea 不受影响。', ztpKeaPool: '管理网段 CIDR', ztpKea239: '239 脚本 URL（含 batch/stage）', ztpKea143: '143 SZTP HTTPS URI', ztpKea60: '60 厂商类 VCI', ztpKeaNOS: '43 NOS 类别', ztpKeaVersion: '43 版本意向', ztpKeaStage: '暂存选项', ztpKeaStaged: '待提交哈希', ztpKeaCommit: '提交仿真选项', ztpKeaActive: '生效仿真哈希', ztpKeaLinkIP: '管理链路 IP（模拟 relay/入口网段）',
    ztpCreate: '创建仿真会话', ztpBatch: '批次', ztpChannel: '引导通道', ztpSerial: '交换机 SN', ztpMgmtMac: '管理口 MAC', ztpBaseMac: '基板 MAC', ztpSessions: '最近会话', ztpNone: '暂无仿真会话。',
    ztpState: '阶段', ztpVerdict: '身份判定', ztpAttempt: '尝试', ztpEvidence: '本步证据引用', ztpAdvance: '推进下一步', ztpFail: '记录失败', ztpRetry: '重试', ztpPhenomenon: '现象', ztpCause: '原因', ztpNextAction: '下一步动作', ztpTimeline: '事件时间线', ztpRetryAt: '最早重试时间', ztpRefresh: '刷新', ztpLoadError: '无法读取 ZTP 会话', ztpActionError: 'ZTP 操作失败',
  },
  en: {
    ztpTab: 'ZTP simulation', ztpTitle: 'ZTP boot simulation', ztpBoundary: 'This timeline is a simulation. Completion does not mean a switch is onboarded or produce network readiness evidence.',
    ztpIsolationManifest: 'Isolation script for review', ztpIsolationApproval: 'Register ztp_script media using this SHA256 and size, then have a separate reviewer approve it. Put the approved media ID in the option 239 policy parameter.', ztpIsolationReview: 'Review script',
    ztpKeaTitle: 'Mock Kea management options', ztpKeaBoundary: 'Simulation only. Commit the staged digest to activate mock offers; real Kea is unaffected.', ztpKeaPool: 'Management CIDR', ztpKea239: '239 script URL (batch/stage)', ztpKea143: '143 SZTP HTTPS URI', ztpKea60: '60 vendor class VCI', ztpKeaNOS: '43 NOS class', ztpKeaVersion: '43 version intent', ztpKeaStage: 'Stage options', ztpKeaStaged: 'Staged digest', ztpKeaCommit: 'Commit mock options', ztpKeaActive: 'Active mock digest', ztpKeaLinkIP: 'Management link IP (relay/ingress simulation)',
    ztpCreate: 'Create simulation session', ztpBatch: 'Batch', ztpChannel: 'Boot channel', ztpSerial: 'Switch serial', ztpMgmtMac: 'Management MAC', ztpBaseMac: 'Base MAC', ztpSessions: 'Recent sessions', ztpNone: 'No simulation sessions yet.',
    ztpState: 'Stage', ztpVerdict: 'Identity verdict', ztpAttempt: 'Attempt', ztpEvidence: 'Stage evidence reference', ztpAdvance: 'Advance stage', ztpFail: 'Record failure', ztpRetry: 'Retry', ztpPhenomenon: 'Phenomenon', ztpCause: 'Cause', ztpNextAction: 'Next action', ztpTimeline: 'Event timeline', ztpRetryAt: 'Earliest retry', ztpRefresh: 'Refresh', ztpLoadError: 'Could not load ZTP sessions', ztpActionError: 'ZTP action failed',
  },
}
