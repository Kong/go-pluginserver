go-pluginserver
===

Runs Kong plugins written in Go.  Implemented as a standalone MessagePack-RPC server.

There's no explicid state associated with the client connections, so the same plugins,
instances and even events could be shared with several clients, or a single client can
use more than one connection from a pool.  For the same reason, plugin instances and
events survive client disconnections.

Pluginserver
--

Holds the running server status.  Starts and stops with the server process.

RPC Methods:

 - `plugin.SetPluginDir()`
 - `plugin.GetPluginInfo()`

Plugin Instance
--

Holds a plugin config object, directly related to each configuration instance.  The config object
is defined by the plugin code, and exported fields (name starts with a caplital letter) are filled
with configuration data and described in the schema.  Any private field is ignored but preserved
between events.

RPC Methods:

 - `plugin.StartInstance()`
 - `plugin.InstanceStatus()`
 - `plugin.CloseInstance()`

Event
--

Handles a Kong event.  The event instance lives during the whole callback/response cyle.
Several events can share a single plugin instance concurrently, exercise care if mutating
shared data.

RPC Methods:

 - `plugin.HandleEvent()`
 - `plugin.Step()`
