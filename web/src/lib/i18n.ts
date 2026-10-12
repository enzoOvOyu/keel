import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import { readPreference } from './preferences'

export const resources = {
  zh: { translation: {
    product: '交付部署平台', title: '登录', brandLine1: '让交付有序，', brandLine2: '让每一步可追溯。',
    brandBody: '从裸金属装机到验证交付，将设备、作业与证据连接在同一条流程中。',
    lifecycle: '贯穿交付全流程', stages: ['装机', '验机', '测试', '交付'],
    tagline: '每一次执行，都有据可查。', welcome: '登录 keel', intro: '进入你的交付工作空间。',
    account: '账号', accountPlaceholder: '输入你的账号', password: '密码', passwordPlaceholder: '输入你的密码',
    showPassword: '显示密码', hidePassword: '隐藏密码', signIn: '登录',
    notConfigured: '身份服务尚未启用', notConfiguredDetail: '暂时无法登录。请联系管理员完成身份服务配置后，再进入工作空间。',
    loading: '正在检查登录方式…', loadingDetail: '正在连接身份服务。',
    loadError: '暂时无法连接身份服务', loadErrorDetail: '请检查连接后重试。你的登录信息不会被发送。',
    unsupported: '登录连接尚未就绪', unsupportedDetail: '身份服务配置发生变化，当前页面尚未启用登录。请联系管理员。',
    retry: '重新检查', retrying: '检查中…', adminHelp: '无法登录？请联系管理员',
    footer: '以可靠的底座，承载每一次交付。', health: '服务状态',
    lightTheme: '切换到浅色主题', darkTheme: '切换到深色主题', language: 'Switch to English',
    skip: '跳转到登录表单',
  } },
  en: { translation: {
    product: 'Delivery & Deployment', title: 'Sign in', brandLine1: 'Order in every delivery.', brandLine2: 'Evidence at every step.',
    brandBody: 'Connect your devices, workflows and evidence — from bare-metal provisioning to verified handover.',
    lifecycle: 'The complete delivery lifecycle', stages: ['Provision', 'Validate', 'Test', 'Deliver'],
    tagline: 'Every execution. A clear record.', welcome: 'Sign in to keel', intro: 'Your delivery workspace starts here.',
    account: 'Account', accountPlaceholder: 'Enter your account', password: 'Password', passwordPlaceholder: 'Enter your password',
    showPassword: 'Show password', hidePassword: 'Hide password', signIn: 'Sign in',
    notConfigured: 'Identity service is not enabled', notConfiguredDetail: 'Sign-in is currently unavailable. Contact your administrator to configure the identity service.',
    loading: 'Checking sign-in methods…', loadingDetail: 'Connecting to the identity service.',
    loadError: 'Identity service is unavailable', loadErrorDetail: 'Check your connection and try again. Your sign-in details will not be sent.',
    unsupported: 'Sign-in connection is not ready', unsupportedDetail: 'The identity configuration has changed. Sign-in is not enabled in this preview. Contact your administrator.',
    retry: 'Check again', retrying: 'Checking…', adminHelp: 'Need access? Contact your administrator',
    footer: 'A reliable foundation for every delivery.', health: 'Service status',
    lightTheme: 'Switch to light theme', darkTheme: 'Switch to dark theme', language: '切换为中文',
    skip: 'Skip to sign-in form',
  } },
}

void i18n.use(initReactI18next).init({
  resources, lng: readPreference('keel.language') === 'en' ? 'en' : 'zh',
  fallbackLng: 'zh', interpolation: { escapeValue: false },
})
export default i18n
