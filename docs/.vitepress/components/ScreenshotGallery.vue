<template>
  <div class="screenshot-gallery-container">
    <div class="screenshot-grid">
      <div 
        v-for="(screenshot, index) in visibleScreenshots" 
        :key="index"
        class="screenshot-card"
        @click="openLightbox(index)"
      >
        <img 
          :src="`/screenshots/screenshot${screenshot}.png`" 
          :alt="`CH-UI Screenshot ${screenshot}`"
          loading="lazy"
        />
        <div class="screenshot-overlay">
          <svg class="zoom-icon" xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="11" cy="11" r="8"></circle>
            <path d="m21 21-4.35-4.35"></path>
            <line x1="11" y1="8" x2="11" y2="14"></line>
            <line x1="8" y1="11" x2="14" y2="11"></line>
          </svg>
        </div>
      </div>
    </div>
    
    <div v-if="totalScreenshots > initialShow" class="show-more-container">
      <button @click="toggleShowAll" class="show-more-btn">
        {{ showAll ? 'Show Less' : `Show All ${totalScreenshots} Screenshots` }}
      </button>
    </div>

    <!-- Lightbox Modal -->
    <Teleport to="body">
      <div v-if="lightboxOpen" class="lightbox-modal" @click="closeLightbox">
        <div class="lightbox-content" @click.stop>
          <button class="lightbox-close" @click="closeLightbox">×</button>
          <button class="lightbox-prev" @click.stop="prevImage" :disabled="currentIndex === 0">‹</button>
          <button class="lightbox-next" @click.stop="nextImage" :disabled="currentIndex === totalScreenshots - 1">›</button>
          <img 
            :src="`/screenshots/screenshot${screenshots[currentIndex]}.png`" 
            :alt="`CH-UI Screenshot ${screenshots[currentIndex]}`"
          />
          <div class="lightbox-caption">
            Screenshot {{ screenshots[currentIndex] }} of {{ totalScreenshots }}
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'

const totalScreenshots = 14
const initialShow = 6
const showAll = ref(false)
const lightboxOpen = ref(false)
const currentIndex = ref(0)

// Generate array of screenshot numbers
const screenshots = Array.from({ length: totalScreenshots }, (_, i) => i + 1)

const visibleScreenshots = computed(() => {
  return showAll.value ? screenshots : screenshots.slice(0, initialShow)
})

const toggleShowAll = () => {
  showAll.value = !showAll.value
}

const openLightbox = (index) => {
  currentIndex.value = index
  lightboxOpen.value = true
  document.body.style.overflow = 'hidden'
}

const closeLightbox = () => {
  lightboxOpen.value = false
  document.body.style.overflow = ''
}

const nextImage = () => {
  if (currentIndex.value < totalScreenshots - 1) {
    currentIndex.value++
  }
}

const prevImage = () => {
  if (currentIndex.value > 0) {
    currentIndex.value--
  }
}

// Handle keyboard navigation
if (typeof window !== 'undefined') {
  window.addEventListener('keydown', (e) => {
    if (lightboxOpen.value) {
      if (e.key === 'Escape') closeLightbox()
      if (e.key === 'ArrowRight') nextImage()
      if (e.key === 'ArrowLeft') prevImage()
    }
  })
}
</script>

<style scoped>
.screenshot-gallery-container {
  margin: 2rem 0;
}

.screenshot-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
  gap: 1.5rem;
  padding: 1rem 0;
}

@media (min-width: 768px) {
  .screenshot-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (min-width: 1024px) {
  .screenshot-grid {
    grid-template-columns: repeat(3, 1fr);
  }
}

.screenshot-card {
  position: relative;
  border-radius: 8px;
  overflow: hidden;
  box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
  transition: all 0.3s ease;
  cursor: pointer;
  background: var(--vp-c-bg-soft);
}

.screenshot-card:hover {
  transform: translateY(-4px);
  box-shadow: 0 12px 24px rgba(0, 0, 0, 0.15);
}

.screenshot-card img {
  width: 100%;
  height: auto;
  display: block;
  transition: transform 0.3s ease;
}

.screenshot-card:hover img {
  transform: scale(1.05);
}

.screenshot-overlay {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.7);
  display: flex;
  align-items: center;
  justify-content: center;
  opacity: 0;
  transition: opacity 0.3s ease;
  pointer-events: none;
}

.screenshot-card:hover .screenshot-overlay {
  opacity: 1;
}

.zoom-icon {
  color: white;
  width: 32px;
  height: 32px;
}

.show-more-container {
  text-align: center;
  margin-top: 2rem;
}

.show-more-btn {
  background: var(--vp-c-brand-1);
  color: white;
  border: none;
  padding: 0.75rem 1.5rem;
  border-radius: 6px;
  font-size: 1rem;
  cursor: pointer;
  transition: all 0.3s ease;
  font-weight: 500;
}

.show-more-btn:hover {
  background: var(--vp-c-brand-2);
  transform: translateY(-2px);
}

/* Lightbox styles */
.lightbox-modal {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.95);
  z-index: 9999;
  display: flex;
  align-items: center;
  justify-content: center;
  animation: fadeIn 0.3s ease;
}

@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

.lightbox-content {
  position: relative;
  max-width: 90vw;
  max-height: 90vh;
  display: flex;
  flex-direction: column;
  align-items: center;
}

.lightbox-content img {
  max-width: 100%;
  max-height: 85vh;
  object-fit: contain;
  border-radius: 8px;
}

.lightbox-close {
  position: absolute;
  top: -40px;
  right: 0;
  background: none;
  border: none;
  color: white;
  font-size: 3rem;
  cursor: pointer;
  padding: 0;
  width: 40px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: color 0.3s ease;
}

.lightbox-close:hover {
  color: var(--vp-c-brand-1);
}

.lightbox-prev,
.lightbox-next {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  background: rgba(255, 255, 255, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.2);
  color: white;
  font-size: 3rem;
  cursor: pointer;
  padding: 1rem 1.5rem;
  border-radius: 4px;
  transition: all 0.3s ease;
  backdrop-filter: blur(10px);
}

.lightbox-prev:hover:not(:disabled),
.lightbox-next:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.2);
  border-color: var(--vp-c-brand-1);
}

.lightbox-prev:disabled,
.lightbox-next:disabled {
  opacity: 0.3;
  cursor: not-allowed;
}

.lightbox-prev {
  left: 20px;
}

.lightbox-next {
  right: 20px;
}

.lightbox-caption {
  color: white;
  margin-top: 1rem;
  font-size: 0.9rem;
  opacity: 0.8;
}

@media (max-width: 768px) {
  .lightbox-prev,
  .lightbox-next {
    padding: 0.5rem 1rem;
    font-size: 2rem;
  }
  
  .lightbox-prev {
    left: 10px;
  }
  
  .lightbox-next {
    right: 10px;
  }
}
</style>