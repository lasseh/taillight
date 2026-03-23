// Prism language components (prism-log, prism-json, etc.) expect a global
// `Prism` object. Vite 8 may reorder chunk evaluation so that components
// run before prismjs sets the global. Importing this module first ensures
// the global is available.
import Prism from 'prismjs'
;(globalThis as any).Prism = Prism
export { Prism }
