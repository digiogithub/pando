"use strict";
// Copyright 2022 Google LLC
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
exports.availabilityZoneAndRegion = exports.hostName = exports.hostId = exports.hostType = exports.onGce = void 0;
/**
 * Implementation in this file copied from
 * https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/blob/v1.8.0/detectors/gcp/gce.go
 */
const api_1 = require("@opentelemetry/api");
const metadata = require("gcp-metadata");
const MACHINE_TYPE_METADATA_ATTR = 'machine-type';
const ID_METADATA_ATTR = 'id';
const HOST_NAME_METADATA_ATTR = 'name';
const ZONE_METADATA_ATTR = 'zone';
async function onGce() {
    try {
        await metadata.instance(MACHINE_TYPE_METADATA_ATTR);
        return true;
    }
    catch (err) {
        api_1.diag.debug('Could not fetch metadata attribute %s, assuming not on GCE. Error was %s', MACHINE_TYPE_METADATA_ATTR, err);
        return false;
    }
}
exports.onGce = onGce;
/**
 * The machine type of the instance on which this program is running. Check that {@link
 * onGce()} is true before calling this, or it may throw exceptions.
 */
async function hostType() {
    return metadata.instance(MACHINE_TYPE_METADATA_ATTR);
}
exports.hostType = hostType;
/**
 * The instance ID of the instance on which this program is running. Check that {@link onGce()}
 * is true before calling this, or it may throw exceptions.
 */
async function hostId() {
    // May be a bignumber.js BigNumber which can just be converted with toString(). See
    // https://github.com/googleapis/gcp-metadata#take-care-with-large-number-valued-properties
    const id = await metadata.instance(ID_METADATA_ATTR);
    return id.toString();
}
exports.hostId = hostId;
/**
 * The instance ID of the instance on which this program is running. Check that {@link onGce()}
 * is true before calling this, or it may throw exceptions.
 */
async function hostName() {
    return metadata.instance(HOST_NAME_METADATA_ATTR);
}
exports.hostName = hostName;
/**
 * The zone and region in which this program is running. Check that {@link onGce()} is true
 * before calling this, or it may throw exceptions.
 */
async function availabilityZoneAndRegion() {
    const fullZone = await metadata.instance(ZONE_METADATA_ATTR);
    // Format described in
    // https://cloud.google.com/compute/docs/metadata/default-metadata-values#vm_instance_metadata
    const re = /projects\/\d+\/zones\/(?<zone>(?<region>\w+-\w+)-\w+)/;
    const { zone, region } = fullZone.match(re)?.groups ?? {};
    if (!zone || !region) {
        throw new Error(`zone was not in the expected format: projects/PROJECT_NUM/zones/COUNTRY-REGION-ZONE. Got ${fullZone}`);
    }
    return { zone, region };
}
exports.availabilityZoneAndRegion = availabilityZoneAndRegion;
//# sourceMappingURL=gce.js.map