package main

import (
    "strings"
    "github.com/godbus/dbus/v5"
)

type Notification interface {
    GetID() uint32
    GetMPRISObject() dbus.BusObject
    Close()
    Update()
    SetProperty(key string, value dbus.Variant) error
    HandleNotificationClosed(reason uint32)
    HandleActionInvoked(action_key string)
}

type notification struct {
    notificationsObject dbus.BusObject
    mprisObject dbus.BusObject
    idChan chan Notification
    id uint32
    identity string
    desktopEntry string
    canQuit bool
    canRaise bool
    canGoNext bool
    canGoPrev bool
    canPlay bool
    canPause bool
    isPlaying bool
    artUrl string
    artist []string
    title string
}

func NewNotification(
    notificationsObject dbus.BusObject,
    mprisObject dbus.BusObject,
    idChan chan Notification,
) Notification {
    self := &notification{}
    self.notificationsObject = notificationsObject
    self.mprisObject = mprisObject
    self.idChan = idChan

    remaining := 2
    ch := make(chan *dbus.Call, remaining)
    self.mprisObject.Go("org.freedesktop.DBus.Properties.GetAll", 0, ch, "org.mpris.MediaPlayer2")
    self.mprisObject.Go("org.freedesktop.DBus.Properties.GetAll", 0, ch,
        "org.mpris.MediaPlayer2.Player",
    )
    for call := range ch {
        var props map[string]dbus.Variant
        err := call.Store(&props)
        if err != nil {
            panic(err)
        }
        for k, v := range props {
            self.SetProperty(k, v)
        }
        remaining--
        if remaining <= 0 {
            break
        }
    }
    self.Update()
    return self
}

func (self *notification) GetID() uint32 {
    return self.id
}

func (self *notification) GetMPRISObject() dbus.BusObject {
    return self.mprisObject
}

func (self *notification) Close() {
    err := self.notificationsObject.Go("org.freedesktop.Notifications.CloseNotification",
        dbus.FlagNoReplyExpected, nil, self.id, /* id */
    ).Err
    if err != nil {
        panic(err)
    }
}

func (self *notification) Update() {
    var actions []string

    if self.canGoPrev {
        actions = append(actions, []string{"media-skip-backward", "Previous"}...)
    }
    if self.isPlaying {
        if self.canPause {
            actions = append(actions, []string{"media-playback-pause", "Pause"}...)
        }
    } else {
        if self.canPlay {
            actions = append(actions, []string{"media-playback-start", "Play"}...)
        }
    }
    if self.canGoNext {
        actions = append(actions, []string{"media-skip-forward", "Next"}...)
    }

    var flags dbus.Flags
    var ch chan *dbus.Call
    if self.id == 0 {
        ch = make(chan *dbus.Call, 1)
    } else {
        flags |= dbus.FlagNoReplyExpected
    }
    err := self.notificationsObject.Go("org.freedesktop.Notifications.Notify", flags, ch,
        self.identity, /* app_name */
        self.id, /* replaces_id */
        "", /* app_icon */
        self.title, /* summary */
        strings.Join(self.artist, ", "), /* body */
        actions, /* actions */
        map[string]dbus.Variant{
            "action-icons": dbus.MakeVariant(true),
            "desktop-entry": dbus.MakeVariant(self.desktopEntry),
            "image-path": dbus.MakeVariant(self.artUrl),
            "resident": dbus.MakeVariant(true),
            "suppress-sound": dbus.MakeVariant(true),
        }, /* hints */
        int32(0), /* expire_timeout */
    ).Err
    if err != nil {
        panic(err)
    }
    if self.id != 0 {
        return
    }
    go func() {
        call := <-ch
        err := call.Store(&self.id)
        if err != nil {
            panic(err)
        }
        self.idChan <- self
    }()
}

func (self *notification) SetProperty(key string, value dbus.Variant) error {
    var err error
    switch key {
    case "Identity":
        err = value.Store(&self.identity)
    case "DesktopEntry":
        err = value.Store(&self.desktopEntry)
    case "CanQuit":
        err = value.Store(&self.canQuit)
    case "CanRaise":
        err = value.Store(&self.canRaise)
    case "CanGoNext":
        err = value.Store(&self.canGoNext)
    case "CanGoPrev":
        err = value.Store(&self.canGoPrev)
    case "CanPlay":
        err = value.Store(&self.canPlay)
    case "CanPause":
        err = value.Store(&self.canPause)
    case "PlaybackStatus":
        var status string
        err = value.Store(&status)
        if err != nil {
            break
        }
        self.isPlaying = status == "Playing"
    case "Metadata":
        var metadata map[string]dbus.Variant
        err = value.Store(&metadata)
        if err != nil {
            break
        }
        storeMeta := func(key string, dest interface{}) {
            field, ok := metadata[key]
            if ok {
                field.Store(dest)
            }
        }
        storeMeta("mpris:artUrl", &self.artUrl)
        storeMeta("xesam:artist", &self.artist)
        storeMeta("xesam:title", &self.title)
    }
    return err
}

func (self *notification) HandleNotificationClosed(reason uint32) {
    if self.canQuit {
        self.mprisObject.Go("org.mpris.MediaPlayer2.Quit", dbus.FlagNoReplyExpected, nil)
    } else {
        self.id = 0
        self.Update()
    }
}

func (self *notification) HandleActionInvoked(actionKey string) {
    switch actionKey {
    case "media-skip-forward":
        self.mprisObject.Go("org.mpris.MediaPlayer2.Player.Next", dbus.FlagNoReplyExpected, nil)
    case "media-skip-backward":
        self.mprisObject.Go("org.mpris.MediaPlayer2.Player.Prev", dbus.FlagNoReplyExpected, nil)
    case "media-playback-pause":
        self.mprisObject.Go("org.mpris.MediaPlayer2.Player.Pause", dbus.FlagNoReplyExpected, nil)
    case "media-playback-start":
        self.mprisObject.Go("org.mpris.MediaPlayer2.Player.Play", dbus.FlagNoReplyExpected, nil)
    default:
        if self.canRaise {
            self.mprisObject.Go("org.mpris.MediaPlayer2.Raise", dbus.FlagNoReplyExpected, nil)
        }
    }
}
