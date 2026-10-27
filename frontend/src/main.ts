import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'virtual:uno.css'
import './themes.css'
import './base.css'
import './content.css'
import App from './App.vue'
import router from './router'
import i18n from './i18n'

createApp(App).use(createPinia()).use(router).use(i18n).use(ElementPlus).mount('#app')
