"use strict";
// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
Object.defineProperty(exports, "__esModule", { value: true });
exports.faasCloudRegion = exports.faasInstance = exports.faasVersion = exports.faasName = exports.onCloudFunctions = exports.onCloudRun = void 0;
/**
 * Implementation in this file copied from
 * https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/blob/v1.8.0/detectors/gcp/faas.go
 */
const metadata = require("gcp-metadata");
const ID_METADATA_ATTR = 'id';
const CLOUD_RUN_CONFIG_ENV = 'K_CONFIGURATION';
const CLOUD_FUNCTION_TARGET_ENV = 'FUNCTION_TARGET';
const FAAS_SERVICE_ENV = 'K_SERVICE';
const FAAS_REVISION_ENV = 'K_REVISION';
const REGION_METADATA_ATTR = 'region';
async function onCloudRun() {
    return process.env[CLOUD_RUN_CONFIG_ENV] !== undefined;
}
exports.onCloudRun = onCloudRun;
async function onCloudFunctions() {
    return process.env[CLOUD_FUNCTION_TARGET_ENV] !== undefined;
}
exports.onCloudFunctions = onCloudFunctions;
/**
 * The name of the Cloud Run or Cloud Function. Check that {@link onCloudRun()} or {@link
 * onCloudFunctions()} is true before calling this, or it may throw exceptions.
 */
async function faasName() {
    return lookupEnv(FAAS_SERVICE_ENV);
}
exports.faasName = faasName;
/**
 * The version/revision of the Cloud Run or Cloud Function. Check that {@link onCloudRun()} or
 * {@link onCloudFunctions()} is true before calling this, or it may throw exceptions.
 */
async function faasVersion() {
    return lookupEnv(FAAS_REVISION_ENV);
}
exports.faasVersion = faasVersion;
/**
 * The ID for the running instance of a Cloud Run or Cloud Function. Check that {@link
 * onCloudRun()} or {@link onCloudFunctions()} is true before calling this, or it may throw
 * exceptions.
 */
async function faasInstance() {
    // May be a bignumber.js BigNumber which can just be converted with toString(). See
    // https://github.com/googleapis/gcp-metadata#take-care-with-large-number-valued-properties
    const id = await metadata.instance(ID_METADATA_ATTR);
    return id.toString();
}
exports.faasInstance = faasInstance;
/**
 * The cloud region where the running instance of a Cloud Run or Cloud Function is located.
 * Check that {@link onCloudRun()} or {@link onCloudFunctions()} is true before calling this,
 * or it may throw exceptions.
 */
async function faasCloudRegion() {
    const region = await metadata.instance(REGION_METADATA_ATTR);
    return region.slice(region.lastIndexOf('/') + 1);
}
exports.faasCloudRegion = faasCloudRegion;
function lookupEnv(key) {
    const val = process.env[key];
    if (val === undefined) {
        throw new Error(`Environment variable ${key} not found`);
    }
    return val;
}
//# sourceMappingURL=faas.js.map