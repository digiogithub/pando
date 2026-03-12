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
exports.numbersToStrings = exports.exp2 = exports.mountProjectIdPath = exports.partitionList = void 0;
/** Returns the minimum number of arrays of max size chunkSize, partitioned from the given array. */
function partitionList(list, chunkSize) {
    const listCopy = [...list];
    const results = [];
    while (listCopy.length) {
        results.push(listCopy.splice(0, chunkSize));
    }
    return results;
}
exports.partitionList = partitionList;
/** Mounts the GCP project id path */
function mountProjectIdPath(projectId) {
    return `projects/${projectId}`;
}
exports.mountProjectIdPath = mountProjectIdPath;
/**
 * Returns the result of 2^value
 */
function exp2(value) {
    return Math.pow(2, value);
}
exports.exp2 = exp2;
/**
 * Map array of numbers to strings
 *
 * @param values an array of numbers
 * @returns a list of strings for those integers
 */
function numbersToStrings(values) {
    return values.map(value => value.toString());
}
exports.numbersToStrings = numbersToStrings;
//# sourceMappingURL=utils.js.map