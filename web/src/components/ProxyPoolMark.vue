<script setup lang="ts">
withDefaults(
  defineProps<{
    compact?: boolean;
  }>(),
  {
    compact: false,
  }
);
</script>

<template>
  <span class="proxy-pool-mark" :class="{ compact }">
    <span class="proxy-pool-mark-glyph">
      <i />
      <i />
      <i />
    </span>
    <span v-if="!compact" class="proxy-pool-mark-word">POOL</span>
  </span>
</template>

<style scoped>
.proxy-pool-mark {
  align-items: center;
  display: inline-flex;
  flex: 0 0 auto;
  gap: 8px;
}

.proxy-pool-mark-glyph {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  border: 1px solid rgba(255, 255, 255, 0.42);
  border-radius: 10px;
  box-shadow: 0 4px 10px rgba(102, 126, 234, 0.2);
  display: block;
  height: 32px;
  position: relative;
  width: 32px;
}

.proxy-pool-mark-glyph i {
  background: #fff;
  border-radius: 99px;
  display: block;
  height: 3px;
  left: 8px;
  position: absolute;
  transform: skewX(-24deg);
  width: 17px;
}

.proxy-pool-mark-glyph i:nth-child(1) {
  top: 8px;
}

.proxy-pool-mark-glyph i:nth-child(2) {
  left: 10px;
  opacity: 0.82;
  top: 14px;
}

.proxy-pool-mark-glyph i:nth-child(3) {
  left: 12px;
  opacity: 0.64;
  top: 20px;
}

.proxy-pool-mark-word {
  color: var(--primary-color, #667eea);
  font-size: 12px;
  font-weight: 800;
  letter-spacing: 0.14em;
  line-height: 1;
}

.proxy-pool-mark.compact .proxy-pool-mark-glyph {
  border-radius: 7px;
  height: 22px;
  width: 22px;
}

.proxy-pool-mark.compact .proxy-pool-mark-glyph i {
  height: 2px;
  left: 5px;
  width: 12px;
}

.proxy-pool-mark.compact .proxy-pool-mark-glyph i:nth-child(1) {
  top: 5px;
}

.proxy-pool-mark.compact .proxy-pool-mark-glyph i:nth-child(2) {
  left: 6px;
  top: 9px;
}

.proxy-pool-mark.compact .proxy-pool-mark-glyph i:nth-child(3) {
  left: 7px;
  top: 13px;
}
</style>
