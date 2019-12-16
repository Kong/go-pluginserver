go-pluginserver
===

Runs Kong plugins written in Go.  Implemented as a standalone MessagePack-RPC server.

There's no explicit state associated with the client connections, so the same plugins,
instances and even events could be shared with several clients, or a single client can
use more than one connection from a pool.  For the same reason, plugin instances and
events could survive client disconnections, if the client reconnects when necessary.

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

The `StartInstance()` method receives configuration data in a serialized format (currently JSON)
in a binary string.  If the configuration is modified externally, a new instance should be started
and the old one closed.

Event
--

Handles a Kong event.  The event instance lives during the whole callback/response cyle.
Several events can share a single plugin instance concurrently, exercise care if mutating
shared data.

RPC Methods:

 - `plugin.HandleEvent()`
 - `plugin.Step()`
 - `plugin.StepError()`
 - `plugin.StepCredential()`
 - `plugin.StepRoute()`
 - `plugin.StepService()`
 - `plugin.StepConsumer()`
 - `plugin.StepMemoryStats()`

To start an event, call `plugin.HandleEvent()` with an instance id and event name.  The return data
will include the event ID and either a `"ret"` string or callback request and parameters.  If the
callback response is a primitive type (number, string, simple dictionary) return it via the
`plugin.Step()` method, including the event ID.  To return an error, use `plugin.StepError()`.
For other specific complex types, use the corresponding `plugin.StepXXX()` method.
