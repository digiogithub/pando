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
exports.ValueType = exports.MetricKind = void 0;
/**
 * The kind of measurement. It describes how the data is reported.
 */
var MetricKind;
(function (MetricKind) {
    MetricKind["UNSPECIFIED"] = "METRIC_KIND_UNSPECIFIED";
    MetricKind["GAUGE"] = "GAUGE";
    MetricKind["DELTA"] = "DELTA";
    MetricKind["CUMULATIVE"] = "CUMULATIVE";
})(MetricKind = exports.MetricKind || (exports.MetricKind = {}));
/** The value type of a metric. */
var ValueType;
(function (ValueType) {
    ValueType["VALUE_TYPE_UNSPECIFIED"] = "VALUE_TYPE_UNSPECIFIED";
    ValueType["INT64"] = "INT64";
    ValueType["DOUBLE"] = "DOUBLE";
    ValueType["DISTRIBUTION"] = "DISTRIBUTION";
})(ValueType = exports.ValueType || (exports.ValueType = {}));
//# sourceMappingURL=types.js.map