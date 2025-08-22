import DefaultTheme from 'vitepress/theme'
import './custom.css'
import ScreenshotGallery from './components/ScreenshotGallery.vue'

export default {
  ...DefaultTheme,
  enhanceApp({ app }) {
    // Register global components
    app.component('ScreenshotGallery', ScreenshotGallery)
  }
}