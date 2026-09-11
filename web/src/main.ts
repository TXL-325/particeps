import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import './assets/main.css'
import LoginView from './views/LoginView.vue'
import OverviewView from './views/OverviewView.vue'
import InstancesView from './views/InstancesView.vue'
import InstanceDetailView from './views/InstanceDetailView.vue'
import SettingsView from './views/SettingsView.vue'
import TaskView from './views/TaskView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: LoginView },
    { path: '/', component: OverviewView },
    { path: '/instances', component: InstancesView },
    { path: '/instances/:id', component: InstanceDetailView, props: true },
    { path: '/settings', component: SettingsView },
    { path: '/tasks/:id', component: TaskView, props: true },
  ],
})

createApp(App).use(router).mount('#app')
