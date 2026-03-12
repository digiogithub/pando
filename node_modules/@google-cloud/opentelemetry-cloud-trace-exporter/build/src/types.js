"use strict";
// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
Object.defineProperty(exports, "__esModule", { value: true });
exports.SpanKind = exports.Code = exports.LinkType = exports.Type = void 0;
var Type;
(function (Type) {
    Type[Type["TYPE_UNSPECIFIED"] = 0] = "TYPE_UNSPECIFIED";
    Type[Type["SENT"] = 1] = "SENT";
    Type[Type["RECEIVED"] = 2] = "RECEIVED";
})(Type = exports.Type || (exports.Type = {}));
var LinkType;
(function (LinkType) {
    LinkType[LinkType["UNSPECIFIED"] = 0] = "UNSPECIFIED";
    LinkType[LinkType["CHILD_LINKED_SPAN"] = 1] = "CHILD_LINKED_SPAN";
    LinkType[LinkType["PARENT_LINKED_SPAN"] = 2] = "PARENT_LINKED_SPAN";
})(LinkType = exports.LinkType || (exports.LinkType = {}));
/**
 * A google.rpc.Code
 */
var Code;
(function (Code) {
    // These are the only two we care about mapping to
    Code[Code["OK"] = 0] = "OK";
    Code[Code["UNKNOWN"] = 2] = "UNKNOWN";
})(Code = exports.Code || (exports.Code = {}));
/**
 * See https://github.com/googleapis/googleapis/blob/8cd4d12c0a02872469176659603451d84c0fbee7/google/devtools/cloudtrace/v2/trace.proto#L182
 */
var SpanKind;
(function (SpanKind) {
    // Unspecified. Do NOT use as default.
    // Implementations MAY assume SpanKind.INTERNAL to be default.
    SpanKind[SpanKind["SPAN_KIND_UNSPECIFIED"] = 0] = "SPAN_KIND_UNSPECIFIED";
    // Indicates that the span is used internally. Default value.
    SpanKind[SpanKind["INTERNAL"] = 1] = "INTERNAL";
    // Indicates that the span covers server-side handling of an RPC or other
    // remote network request.
    SpanKind[SpanKind["SERVER"] = 2] = "SERVER";
    // Indicates that the span covers the client-side wrapper around an RPC or
    // other remote request.
    SpanKind[SpanKind["CLIENT"] = 3] = "CLIENT";
    // Indicates that the span describes producer sending a message to a broker.
    // Unlike client and  server, there is no direct critical path latency
    // relationship between producer and consumer spans (e.g. publishing a
    // message to a pubsub service).
    SpanKind[SpanKind["PRODUCER"] = 4] = "PRODUCER";
    // Indicates that the span describes consumer receiving a message from a
    // broker. Unlike client and  server, there is no direct critical path
    // latency relationship between producer and consumer spans (e.g. receiving
    // a message from a pubsub service subscription).
    SpanKind[SpanKind["CONSUMER"] = 5] = "CONSUMER";
})(SpanKind = exports.SpanKind || (exports.SpanKind = {}));
//# sourceMappingURL=types.js.map