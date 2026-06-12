import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import './style.css'

import Dashboard from './pages/Dashboard.vue'
import Login from './pages/Login.vue'
import Keys from './pages/Keys.vue'
import ProviderDetail from './pages/ProviderDetail.vue'
import Requests from './pages/Requests.vue'
import Errors from './pages/Errors.vue'
import Captures from './pages/Captures.vue'
import Rules from './pages/Rules.vue'
import Settings from './pages/Settings.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/dashboard' },
    { path: '/dashboard', component: Dashboard },
    { path: '/login', component: Login },
    { path: '/keys', component: Keys },
    { path: '/providers/:name', component: ProviderDetail },
    { path: '/requests', component: Requests },
    { path: '/errors', component: Errors },
    { path: '/captures', component: Captures },
    { path: '/rules', component: Rules },
    { path: '/settings', component: Settings },
  ],
})

createApp(App).use(router).mount('#app')
