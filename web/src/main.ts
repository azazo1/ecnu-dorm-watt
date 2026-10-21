import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import { initTheme } from './stores/theme'
import './styles/global.css'

initTheme()

createApp(App).use(router).mount('#app')
