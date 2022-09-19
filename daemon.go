package main

import (
    "fmt"
    "os"
    "strings"
    "github.com/godbus/dbus/v5"
)

var idChan chan Notification
var conn *dbus.Conn
var notificationsObject dbus.BusObject
var notifications map[uint32]Notification
var players map[string]Notification

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
        obj := conn.Object(name, "/org/mpris/MediaPlayer2")
        n = NewNotification(notificationsObject, obj, idChan)
    }

    newOwner := signal.Body[2].(string) /* new_owner */
    if newOwner != "" {
        players[newOwner] = n
    } else {
        delete(notifications, n.GetID())
        n.Close()
    }
}

func onPropertiesChanged(signal *dbus.Signal) {
    n := players[signal.Sender]
    if n == nil {
        return
    }
    n.HandlePropertiesChanged(
        signal.Body[0].(string), /* interface_name */
        signal.Body[1].(map[string]dbus.Variant), /* changed_properties */
        signal.Body[2].([]string), /* invalidated_properties */
    )
}

func onNotificationClosed(signal *dbus.Signal) {
    n := notifications[signal.Body[0].(uint32)] /* id */
    if n == nil {
        return
    }
    n.HandleNotificationClosed(signal.Body[1].(uint32))
}

func onActionInvoked(signal *dbus.Signal) {
    n := notifications[signal.Body[0].(uint32)] /* id */
    if n == nil {
        return
    }
    n.HandleActionInvoked(signal.Body[1].(string))
}

func main() {
    var err error
    conn, err = dbus.ConnectSessionBus()
    if err != nil {
        fmt.Fprintln(os.Stderr, "failed to connect to session bus:", err)
        os.Exit(1)
    }
    defer conn.Close()

    idChan = make(chan Notification, 16)
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

    signalChan := make(chan *dbus.Signal, 16)
    conn.Signal(signalChan)
    var names []string
    err = conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names)
    if err != nil {
        fmt.Fprintln(os.Stderr, "failed to enumerate bus names:", err)
        os.Exit(1)
    }
    notifications = make(map[uint32]Notification, len(names))
    players = make(map[string]Notification, len(names))
    ch := make(chan *dbus.Call, len(names))
    for _, name := range names {
        if strings.HasPrefix(name, "org.mpris.MediaPlayer2.") {
            conn.BusObject().Go("org.freedesktop.DBus.GetNameOwner", 0, ch, name)
        }
    }
    for {
        select {
        case call := <-ch:
            name := call.Args[0].(string)
            var owner string
            err = call.Store(&owner)
            if err != nil {
                panic(err)
            }
            obj := conn.Object(name, "/org/mpris/MediaPlayer2")
            players[owner] = NewNotification(notificationsObject, obj, idChan)
        case signal := <-signalChan:
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
        case n := <-idChan:
            id := n.GetID()
            notifications[id] = n
        }
    }
}
