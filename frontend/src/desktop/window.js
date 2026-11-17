// Window operations resolve through the installed Desktop Adapter so tests use
// the same interface without importing or booting the native runtime.
export const Quit = () => window.runtime.Quit();
export const WindowIsMaximised = () => window.runtime.WindowIsMaximised();
export const WindowMinimise = () => window.runtime.WindowMinimise();
export const WindowToggleMaximise = () => window.runtime.WindowToggleMaximise();
