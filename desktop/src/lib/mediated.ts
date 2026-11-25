import { Role } from "./api/gen/unison/local/v1/local_pb";

/**
 * roleLimit returns the strongest role two devices can be introduced with.
 * Friends is weaker than Personal, so one Friends device limits the new pair.
 * An unknown role fails closed to Friends.
 */
export function roleLimit(one: Role, other: Role): Role {
  return one === Role.PERSONAL && other === Role.PERSONAL ? Role.PERSONAL : Role.FRIENDS;
}
