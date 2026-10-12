// C API of the AppKit layer. Every dm_* UI function may be called from any
// thread; it hops to the main thread itself. Callbacks into Go (go*) are made
// on the main thread and must return quickly.
#ifndef DICTATOR_MAC_H
#define DICTATOR_MAC_H

#include <stdint.h>

// --- app lifecycle (app.m)
void dm_run(void);   // must be called on the main thread; never returns
void dm_quit(void);
void dm_open_url(const char *url);
void dm_sound(const char *name);           // "Tink", "Pop", "Basso", ...
int dm_login_status(void);                 // -1 not in an app bundle, 0 off, 1 on, 2 needs approval
int dm_login_set(int on);                  // returns 0 on success
int dm_in_bundle(void);
void dm_about(const char *version);

// --- menu-bar item (statusitem.m)
enum {
	DM_ITEM_STATUS = 1,
	DM_ITEM_ADD_KEY,
	DM_ITEM_MODE_TOGGLE,
	DM_ITEM_MODE_PTT,
	DM_ITEM_CLEANUP,
	DM_ITEM_LOGIN,
	DM_ITEM_COPY_RAW,
	DM_ITEM_SETTINGS,
	DM_ITEM_LOG,
	DM_ITEM_ABOUT,
	DM_ITEM_QUIT,
};
enum { DM_ICON_IDLE = 0, DM_ICON_RECORDING, DM_ICON_BUSY, DM_ICON_ERROR };
void dm_status_init(void);
void dm_status_icon(int state);
void dm_item_title(int tag, const char *title);
void dm_item_checked(int tag, int on);
void dm_item_enabled(int tag, int on);
void dm_item_hidden(int tag, int hidden);

// --- global hotkey (hotkey.m)
int dm_hotkey_register(uint32_t keycode, uint32_t modifiers); // OSStatus
void dm_hotkey_unregister(void);
void dm_hotkey_suspend(int on); // 1: release the hotkey (while recording a new one), 0: restore it

// --- floating HUD (hud.m)
enum { DM_HUD_HIDE = 0, DM_HUD_RECORDING, DM_HUD_WORKING, DM_HUD_POLISHING, DM_HUD_SUCCESS, DM_HUD_ERROR };
void dm_hud_show(int kind, const char *text, int autohide_ms, int corner);

// --- text insertion (insert.m)
int dm_ax_trusted(void);
void dm_ax_prompt(void);
void dm_type_text(const char *utf8, int chunk, int delay_ms); // call off the main thread
void dm_press_cmd_v(void);
void dm_clipboard_set(const char *utf8);
char *dm_clipboard_get(void); // malloc'd, caller frees; NULL if no text
int dm_frontmost_pid(void);   // the app receiving keystrokes (0 if unknown)

// --- settings window (settings.m)
void dm_settings_open(const char *json);
void dm_settings_status(const char *field, const char *text, int ok);
void dm_settings_close(void);

#endif
