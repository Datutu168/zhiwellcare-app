import { createRouter, createWebHashHistory } from 'vue-router'
import { initializeAppServices } from './AppServices'
import DeveloperDebugView from '../views/DeveloperDebugView.vue'
import HistoryView from '../views/HistoryView.vue'
import ResultView from '../views/ResultView.vue'
import RomCalibrationView from '../views/RomCalibrationView.vue'
import SettingsView from '../views/SettingsView.vue'
import TrainingView from '../views/TrainingView.vue'
// 五大导航 Hub（由对应模块负责人实现，路由先行约定文件位置）
import DeviceView from '../views/DeviceView.vue'
import GameSelectView from '../views/GameSelectView.vue'
import CoursesHomeView from '../views/courses/CoursesHomeView.vue'
import CourseDetailView from '../views/courses/CourseDetailView.vue'
import MallHomeView from '../views/mall/MallHomeView.vue'
import ProductDetailView from '../views/mall/ProductDetailView.vue'
import MineHomeView from '../views/mine/MineHomeView.vue'
import OrdersView from '../views/mine/OrdersView.vue'
import DisclaimerView from '../views/mine/DisclaimerView.vue'
import ChangePasswordView from '../views/mine/ChangePasswordView.vue'
import LoginView from '../views/auth/LoginView.vue'
import RegisterView from '../views/auth/RegisterView.vue'

/** Hash 路由兼容 Tauri/Capacitor 本地文件与 Vite 预览环境；5 大底部导航。 */
export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/devices' },
    // ── 5 大底部导航 ──────────────────────────────
    { path: '/devices', component: DeviceView },                                   // 设备首页（IoT 中枢）
    { path: '/games', component: GameSelectView },                                 // 训练游戏（动态标签匹配）
    { path: '/courses', component: CoursesHomeView },                              // 课程训练
    { path: '/courses/:courseId', component: CourseDetailView },                   // 课程详情
    { path: '/mall', component: MallHomeView },                                    // 商城（硬件 + 服务）
    { path: '/mall/product/:productId', component: ProductDetailView },            // 商品详情
    { path: '/mine', component: MineHomeView },                                    // 我的
    { path: '/mine/orders', component: OrdersView },                               // 订单聚合
    { path: '/mine/disclaimer', component: DisclaimerView },                       // 免责声明 / 隐私
    { path: '/mine/password', component: ChangePasswordView },                     // 修改密码（需登录）
    // ── 账号 ─────────────────────────────────────
    { path: '/auth/login', component: LoginView, meta: { hideChrome: true } },     // 登录
    { path: '/auth/register', component: RegisterView, meta: { hideChrome: true } }, // 注册
    // ── 功能页（Tab 内二级） ───────────────────────
    { path: '/mine/history', component: HistoryView },                             // 训练数据 / 历史
    { path: '/mine/settings', component: SettingsView },                           // 设置（含更新）
    { path: '/mine/settings/debug', component: DeveloperDebugView },               // 调试（开发用）
    { path: '/calibration', component: RomCalibrationView },                       // 个人活动范围与自然中心确认
    // ── 沉浸页（隐藏底部导航） ─────────────────────
    { path: '/training/:gameId', component: TrainingView, meta: { hideChrome: true, trainingLayout: true } },
    { path: '/result', component: ResultView, meta: { hideChrome: true } },
    // ── 兼容旧入口 ────────────────────────────────
    { path: '/setup', redirect: '/devices' },
    { path: '/device', redirect: '/devices' },
    { path: '/:pathMatch(.*)*', redirect: '/devices' },
  ],
})

/** 首次导航前完成服务初始化（Profile/目录/绑定/上报队列）；训练与游戏不再做强 ROM 前置拦截。 */
router.beforeEach(async () => {
  await initializeAppServices()
  return true
})
