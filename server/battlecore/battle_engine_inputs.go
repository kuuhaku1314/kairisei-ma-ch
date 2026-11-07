package battlecore

// Existing call sites share the core's value inputs while implementation files
// move out of multiplayer. No room/network metadata is carried into the core.
type cardPlaySubmission = CardSelection
type burstSkillSubmission = BurstSelection

const maxRoomMembers = ParticipantCount
const deckSphereSlots = SphereSlots

type BattleCard = Card
type BattleSphere = Sphere
type BattleBuddy = Buddy
type BattleDrop = Drop
