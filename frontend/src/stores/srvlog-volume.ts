import { api } from '@/lib/api'
import { createVolumeStore } from '@/stores/volume-store'

export const useSrvlogVolumeStore = createVolumeStore('srvlog-volume', api.getSrvlogVolume, 'hosts')
