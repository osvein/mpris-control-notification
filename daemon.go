package main

import (
    "fmt"
    "os"
    "strings"
    "github.com/godbus/dbus/v5"
)

type Notification struct {
    busObject dbus.BusObject
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

var conn *dbus.Conn
var notificationsObject dbus.BusObject
var notifications map[uint32]*Notification
var players map[string]*Notification

func (self *Notification) Close() {
    delete(notifications, self.id)
    err := notificationsObject.Go("org.freedesktop.Notifications.CloseNotification",
        dbus.FlagNoReplyExpected, nil, uint32(self.id),
    ).Err
    if err != nil {
        panic(err)
    }
}

func (self *Notification) Update() {
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

    err := notificationsObject.Call("org.freedesktop.Notifications.Notify", 0,
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
    ).Store(&self.id)
    if err != nil {
        panic(err)
    }
    notifications[self.id] = self
}

func (self *Notification) SetProperty(key string, value dbus.Variant) error {
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

func (self *Notification) Reset() {
    remaining := 2
    ch := make(chan *dbus.Call, remaining)
    self.busObject.Go("org.freedesktop.DBus.Properties.GetAll", 0, ch, "org.mpris.MediaPlayer2")
    self.busObject.Go("org.freedesktop.DBus.Properties.GetAll", 0, ch,
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
}

func onNameOwnerChanged(signal *dbus.Signal) {
    name := signal.Body[0].(string) /* name */
    if !strings.HasPrefix(name, "org.mpris.MediaPlayer2.") {
        return
    }

    oldOwner := signal.Body[1].(string) /* old_owner */
    n := players[oldOwner]
    if n != nil {
        delete(players, oldOwner)
    } else {
        n = &Notification{}
        n.busObject = conn.Object(name, "/org/mpris/MediaPlayer2")
        n.Reset()
    }

    newOwner := signal.Body[2].(string) /* new_owner */
    if newOwner != "" {
        players[newOwner] = n
    } else {
        n.Close()
    }
}

func onPropertiesChanged(signal *dbus.Signal) {
    n := players[signal.Sender]
    if n == nil {
        return
    }
    interfaceName := signal.Body[0]

    /* changed_properties */
    for k, v := range signal.Body[1].(map[string]dbus.Variant) {
        n.SetProperty(k, v)
    }

    /* invalidated_properties */
    props := signal.Body[2].([]string)
    remaining := len(props)
    if remaining > 0 {
        ch := make(chan *dbus.Call, remaining)
        for k := range props {
            n.busObject.Go("org.freedesktop.DBus.Properties.Get", 0, ch,
                interfaceName, /* interface_name */
                k, /* property_name */
            )
        }
        for call := range ch {
            k := call.Args[1].(string) /* property_name */
            v := dbus.MakeVariant(call.Body[0]) /* value */
            n.SetProperty(k, v)
            remaining--
            if remaining <= 0 {
                break
            }
        }
    }
    n.Update()
}

func onNotificationClosed(signal *dbus.Signal) {
    n := notifications[signal.Body[0].(uint32)] /* id */
    if n == nil {
        return
    }
    if n.canQuit {
        n.busObject.Go("org.mpris.MediaPlayer2.Quit", dbus.FlagNoReplyExpected, nil)
    } else {
        delete(notifications, n.id)
        n.id = 0
        n.Update()
    }
}

func onActionInvoked(signal *dbus.Signal) {
    n := notifications[signal.Body[0].(uint32)] /* id */
    if n == nil {
        return
    }
    switch signal.Body[1] {
    case "media-skip-forward":
        n.busObject.Go("org.mpris.MediaPlayer2.Player.Next", dbus.FlagNoReplyExpected, nil)
    case "media-skip-backward":
        n.busObject.Go("org.mpris.MediaPlayer2.Player.Prev", dbus.FlagNoReplyExpected, nil)
    case "media-playback-pause":
        n.busObject.Go("org.mpris.MediaPlayer2.Player.Pause", dbus.FlagNoReplyExpected, nil)
    case "media-playback-start":
        n.busObject.Go("org.mpris.MediaPlayer2.Player.Play", dbus.FlagNoReplyExpected, nil)
    case n.canRaise:
        n.busObject.Go("org.mpris.MediaPlayer2.Raise", dbus.FlagNoReplyExpected, nil)
    }
}

func main() {
    var err error
    conn, err = dbus.ConnectSessionBus()
    if err != nil {
        fmt.Fprintln(os.Stderr, "failed to connect to session bus:", err)
        os.Exit(1)
    }
    defer conn.Close()

    notificationsObject = conn.Object("org.freedesktop.Notifications",
        "/org/freedesktop/Notifications",
    )

    err = conn.AddMatchSignal(
        dbus.WithMatchObjectPath("/org/freedesktop/DBus"),
        dbus.WithMatchMember("NameOwnerChanged"),
    )
    if err != nil {
        panic(err)
    }
    err = conn.AddMatchSignal(
        dbus.WithMatchObjectPath("/org/freedesktop/Notifications"),
        dbus.WithMatchInterface("org.freedesktop.Notifications"),
    )
    if err != nil {
        panic(err)
    }
    err = conn.AddMatchSignal(
        dbus.WithMatchObjectPath("/org/mpris/MediaPlayer2"),
        dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
        dbus.WithMatchMember("PropertiesChanged"),
    )
    if err != nil {
        panic(err)
    }

    ch := make(chan *dbus.Signal, 16)
    conn.Signal(ch)
    var names []string
    err = conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names)
    if err != nil {
        fmt.Fprintln(os.Stderr, "failed to enumerate bus names:", err)
        os.Exit(1)
    }
    notifications = make(map[uint32]*Notification, len(names))
    players = make(map[string]*Notification, len(names))
    ch2 := make(chan *dbus.Call, len(names))
    count := 0
    for _, name := range names {
        if strings.HasPrefix(name, "org.mpris.MediaPlayer2.") {
            conn.BusObject().Go("org.freedesktop.DBus.GetNameOwner", 0, ch2, name)
            count++
        }
    }
    if count > 0 {
        for call := range ch2 {
            name := call.Args[0].(string)
            var owner string
            err = call.Store(&owner)
            if err != nil {
                panic(err)
            }
            var n Notification
            n.busObject = conn.Object(name, "/org/mpris/MediaPlayer2")
            n.Reset()
            players[owner] = &n
            count--
            if count <= 0 {
                break
            }
        }
    }
    for signal := range ch {
        switch signal.Name {
        case "org.freedesktop.DBus.NameOwnerChanged":
            onNameOwnerChanged(signal)
        case "org.freedesktop.DBus.Properties.PropertiesChanged":
            onPropertiesChanged(signal)
        case "org.freedesktop.Notifications.NotificationClosed":
            onNotificationClosed(signal)
        case "org.freedesktop.Notifications.ActionInvoked":
            onActionInvoked(signal)
        }
    }
}
