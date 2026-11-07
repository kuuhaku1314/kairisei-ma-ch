package battlecore

// Original selector feature slots and target policies. Similar names do not
// imply identical features: DAMAGE_CUT and ALL_REWRITE retain native quirks.
// See ab9bc and the 122200 dispatch table (ELF addresses are 0x10000 lower).
type npcSelectorRule struct {
	feature     int
	policy      string
	parameter   string
	allyFeature int // First of the three ally-release features; zero disables the gate.
	parts       bool
}

var npcAuxiliarySelectors = map[string]npcSelectorRule{
	"ATTACK_RANDOM":                                          {feature: 0, policy: "enemyRandom"},    // 000ae8b8
	"ATTACK_ALL":                                             {feature: 1, policy: "defaultAlive"},   // 000af513
	"BUFF_SELF_ATK_UP":                                       {feature: 15, policy: "self"},          // 000b3d93
	"BUFF_SELF_MAX_HP_UP":                                    {feature: 17, policy: "self"},          // 000b3e04
	"BUFF_SELF_INT_UP":                                       {feature: 18, policy: "self"},          // 000b3e75
	"BUFF_SELF_MND_UP":                                       {feature: 20, policy: "self"},          // 000b3ee6
	"BUFF_SELF_DEF_UP":                                       {feature: 21, policy: "self"},          // 000b3f57
	"BUFF_SELF_DAMAGE_UP":                                    {feature: 25, policy: "self"},          // 000b4039
	"BUFF_SELF_DAMAGE_CUT":                                   {feature: 25, policy: "self"},          // 000b40aa
	"BUFF_ONE_RANDOM_ATK_UP":                                 {feature: 33, policy: "friendRandom"},  // 000b46f0
	"BUFF_ONE_RANDOM_MAX_HP_UP":                              {feature: 34, policy: "friendRandom"},  // 000b47b4
	"BUFF_ONE_RANDOM_INT_UP":                                 {feature: 35, policy: "friendRandom"},  // 000b4878
	"BUFF_ONE_RANDOM_MND_UP":                                 {feature: 36, policy: "friendRandom"},  // 000b493c
	"BUFF_ONE_RANDOM_DEF_UP":                                 {feature: 37, policy: "friendRandom"},  // 000b4a00
	"BUFF_ONE_RANDOM_DAMAGE_UP":                              {feature: 39, policy: "friendRandom"},  // 000b4b88
	"BUFF_ONE_RANDOM_DAMAGE_CUT":                             {feature: 40, policy: "friendRandom"},  // 000b4c4c
	"BUFF_ALL_ATK_UP":                                        {feature: 44, policy: "default"},       // 000b5b18
	"BUFF_ALL_MAX_HP_UP":                                     {feature: 45, policy: "default"},       // 000b5bc1
	"BUFF_ALL_INT_UP":                                        {feature: 46, policy: "default"},       // 000b5c6a
	"BUFF_ALL_MND_UP":                                        {feature: 47, policy: "default"},       // 000b5d13
	"BUFF_ALL_DEF_UP":                                        {feature: 48, policy: "default"},       // 000b5dbc
	"BUFF_ALL_DAMAGE_UP":                                     {feature: 50, policy: "default"},       // 000b5f0e
	"BUFF_ALL_DAMAGE_CUT":                                    {feature: 51, policy: "default"},       // 000b5fb7
	"DEBUFF_ONE_RANDOM_ATK_DOWN":                             {feature: 55, policy: "enemyRandom"},   // 000b6456
	"DEBUFF_ONE_RANDOM_MAX_HP_DOWN":                          {feature: 56, policy: "enemyRandom"},   // 000b64e5
	"DEBUFF_ONE_RANDOM_INT_DOWN":                             {feature: 57, policy: "enemyRandom"},   // 000b6574
	"DEBUFF_ONE_RANDOM_MND_DOWN":                             {feature: 58, policy: "enemyRandom"},   // 000b6603
	"DEBUFF_ONE_RANDOM_DEF_DOWN":                             {feature: 59, policy: "enemyRandom"},   // 000b6692
	"DEBUFF_ONE_RANDOM_MDEF_DOWN":                            {feature: 60, policy: "enemyRandom"},   // 000b6721
	"DEBUFF_ONE_RANDOM_DAMAGE_DOWN":                          {feature: 61, policy: "enemyRandom"},   // 000b67b0
	"DEBUFF_ALL_ATK_DOWN":                                    {feature: 63, policy: "default"},       // 000b7c88
	"DEBUFF_ALL_MAX_HP_DOWN":                                 {feature: 64, policy: "default"},       // 000b7d31
	"DEBUFF_ALL_INT_DOWN":                                    {feature: 65, policy: "default"},       // 000b7dda
	"DEBUFF_ALL_MND_DOWN":                                    {feature: 66, policy: "default"},       // 000b7e83
	"DEBUFF_ALL_DEF_DOWN":                                    {feature: 67, policy: "default"},       // 000b7f2c
	"DEBUFF_ALL_MDEF_DOWN":                                   {feature: 68, policy: "default"},       // 000b7fd5
	"DEBUFF_ALL_DAMAGE_DOWN":                                 {feature: 69, policy: "default"},       // 000b807e
	"GOOD_STATUS_SELF_REWRITE":                               {feature: 76, policy: "self"},          // 000b9305
	"GOOD_STATUS_ONE_RANDOM_REWRITE":                         {feature: 77, policy: "friendRandom"},  // 000b9379
	"GOOD_STATUS_ALL_REWRITE":                                {feature: 77, policy: "defaultAlive"},  // 000b943d
	"BAD_STATUS_ALL_UNERMINE":                                {feature: 95, policy: "default"},       // 000bdfd9
	"BAD_STATUS_ALL_STAN":                                    {feature: 96, policy: "default"},       // 000be082
	"BAD_STATUS_ALL_SILENCE":                                 {feature: 97, policy: "default"},       // 000be12b
	"BAD_STATUS_ALL_CHARM":                                   {feature: 98, policy: "default"},       // 000be1d4
	"BAD_STATUS_ALL_POISON":                                  {feature: 99, policy: "default"},       // 000be27d
	"BAD_STATUS_ALL_BURN":                                    {feature: 100, policy: "default"},      // 000be326
	"BAD_STATUS_ALL_FREEZE":                                  {feature: 101, policy: "default"},      // 000be3cf
	"BAD_STATUS_ALL_BLEED":                                   {feature: 102, policy: "default"},      // 000be478
	"BUFF_SELF_ATK_UP_ONE_TURN":                              {feature: 16, policy: "self"},          // 000b43d3
	"BUFF_SELF_INT_UP_ONE_TURN":                              {feature: 19, policy: "self"},          // 000b4444
	"BUFF_SELF_DEF_UP_ONE_TURN":                              {feature: 22, policy: "self"},          // 000b44b5
	"BUFF_SELF_DAMAGE_UP_ONE_TURN":                           {feature: 26, policy: "self"},          // 000b4597
	"BUFF_SELF_DAMAGE_CUT_ONE_TURN":                          {feature: 28, policy: "self"},          // 000b4608
	"BUFF_SELF_MDEF_UP":                                      {feature: 23, policy: "self"},          // 000b3fc8
	"BUFF_ONE_RANDOM_MDEF_UP":                                {feature: 38, policy: "friendRandom"},  // 000b4ac4
	"BUFF_ALL_MDEF_UP":                                       {feature: 49, policy: "default"},       // 000b5e65
	"BUFF_SELF_MDEF_UP_ONE_TURN":                             {feature: 24, policy: "self"},          // 000b4526
	"GOOD_STATUS_SELF_COVERING":                              {feature: 79, policy: "self"},          // 000bf157
	"GOOD_STATUS_SELF_ATTACK_BARRIER":                        {feature: 80, policy: "self"},          // 000bf1cb
	"GOOD_STATUS_ONE_RANDOM_ATTACK_BARRIER":                  {feature: 81, policy: "friendRandom"},  // 000bf23f
	"GOOD_STATUS_ALL_ATTACK_BARRIER":                         {feature: 82, policy: "defaultAlive"},  // 000bf303
	"DEBUFF_RELEASE_SELF":                                    {feature: 104, policy: "self"},         // 000bf9f6
	"DEBUFF_RELEASE_ALL":                                     {feature: 106, policy: "defaultAlive"}, // 000bfb82
	"BAD_STATUS_ALL_ELECTRIC":                                {feature: 120, policy: "default"},      // 000be521
	"BUFF_SELF_CRITICAL_UP":                                  {feature: 29, policy: "self"},          // 000b411b
	"BUFF_ALL_CRITICAL_UP":                                   {feature: 52, policy: "default"},       // 000b6060
	"BUFF_SELF_CRITICAL_UP_ONE_TURN":                         {feature: 30, policy: "self"},          // 000b467c
	"BAD_STATUS_ALL_CARD_TRAP_DOT":                           {feature: 127, policy: "default"},      // 000be5ca
	"DEBUFF_ONE_RANDOM_CARD_TRAP_DAMAGE":                     {feature: 128, policy: "enemyRandom"},  // 000b683f
	"DEBUFF_ALL_CARD_TRAP_DAMAGE":                            {feature: 129, policy: "default"},      // 000b8127
	"BUFF_SELF_ENCHANT":                                      {feature: 31, policy: "self"},          // 000b418f
	"BUFF_ALL_ENCHANT":                                       {feature: 53, policy: "default"},       // 000b6109
	"BUFF_SELF_ATTR_DEF_UP":                                  {feature: 32, policy: "self"},          // 000b4203
	"BUFF_ONE_RANDOM_ATTR_DEF_UP":                            {feature: 43, policy: "friendRandom"},  // 000b4d10
	"BUFF_ALL_ATTR_DEF_UP":                                   {feature: 54, policy: "default"},       // 000b61b2
	"DEBUFF_ONE_RANDOM_ATTR_DEF_DOWN":                        {feature: 62, policy: "enemyRandom"},   // 000b68ce
	"DEBUFF_ALL_ATTR_DEF_DOWN":                               {feature: 70, policy: "default"},       // 000b81d0
	"GOOD_STATUS_SELF_REFLECTION":                            {feature: 83, policy: "self"},          // 000bf37f
	"GOOD_STATUS_ONE_RANDOM_REFLECTION":                      {feature: 84, policy: "friendRandom"},  // 000bf3f3
	"GOOD_STATUS_ALL_REFLECTION":                             {feature: 85, policy: "defaultAlive"},  // 000bf59c
	"BLESS_SELF":                                             {feature: 187, policy: "self"},         // 000c2ab3
	"BLESS_ONE_RANDOM":                                       {feature: 188, policy: "friendRandom"}, // 000c2b27
	"BLESS_ALL":                                              {feature: 189, policy: "defaultAlive"}, // 000c2beb
	"BUFF_SELF_ATK_LIMIT_BREAK":                              {feature: 190, policy: "self"},         // 000b4277
	"BUFF_ONE_RANDOM_ATK_LIMIT_BREAK":                        {feature: 191, policy: "friendRandom"}, // 000b4dd4
	"BUFF_ALL_ATK_LIMIT_BREAK":                               {feature: 192, policy: "default"},      // 000b625b
	"BUFF_SELF_INT_LIMIT_BREAK":                              {feature: 193, policy: "self"},         // 000b42eb
	"BUFF_ONE_RANDOM_INT_LIMIT_BREAK":                        {feature: 194, policy: "friendRandom"}, // 000b4e98
	"BUFF_ALL_INT_LIMIT_BREAK":                               {feature: 195, policy: "default"},      // 000b6304
	"BUFF_SELF_MND_LIMIT_BREAK":                              {feature: 196, policy: "self"},         // 000b435f
	"BUFF_ALL_MND_LIMIT_BREAK":                               {feature: 198, policy: "default"},      // 000b63ad
	"GOOD_STATUS_SELF_GUTS":                                  {feature: 199, policy: "defaultAlive"}, // 000bf618
	"GOOD_STATUS_ONE_RANDOM_GUTS":                            {feature: 200, policy: "friendRandom"}, // 000bf694
	"GOOD_STATUS_ALL_GUTS":                                   {feature: 201, policy: "defaultAlive"}, // 000bf97a
	"BUFF_ONE_RANDOM_ATTR_CUT_ATTACK":                        {feature: 203, policy: "friendRandom"}, // 000c3919
	"BUFF_ALL_ATTR_CUT_ATTACK":                               {feature: 204, policy: "defaultAlive"}, // 000c4950
	"BUFF_ONE_RANDOM_ATTR_CUT_ENCHANT":                       {feature: 206, policy: "friendRandom"}, // 000c39dd
	"BUFF_ALL_ATTR_CUT_ENCHANT":                              {feature: 207, policy: "defaultAlive"}, // 000c49cc
	"BUFF_ALL_ATTR_CUT_DOT_POISON":                           {feature: 210, policy: "defaultAlive"}, // 000c4a48
	"BUFF_ALL_ATTR_CUT_DOT_BURN":                             {feature: 213, policy: "defaultAlive"}, // 000c4ac4
	"BUFF_ALL_ATTR_CUT_DOT_FREEZE":                           {feature: 216, policy: "defaultAlive"}, // 000c4b40
	"BUFF_ALL_ATTR_CUT_DOT_BLEED":                            {feature: 219, policy: "defaultAlive"}, // 000c4bbc
	"BUFF_ALL_ATTR_CUT_DOT_ELECTRIC":                         {feature: 222, policy: "defaultAlive"}, // 000c4c38
	"BUFF_ONE_RANDOM_ATTR_DRAIN_ATTACK":                      {feature: 224, policy: "friendRandom"}, // 000c3aa1
	"BUFF_ALL_ATTR_DRAIN_ATTACK":                             {feature: 225, policy: "defaultAlive"}, // 000c4cb4
	"BUFF_ONE_RANDOM_ATTR_DRAIN_ENCHANT":                     {feature: 227, policy: "friendRandom"}, // 000c3b65
	"BUFF_ALL_ATTR_DRAIN_ENCHANT":                            {feature: 228, policy: "defaultAlive"}, // 000c4d30
	"BUFF_ALL_ATTR_DRAIN_DOT_POISON":                         {feature: 231, policy: "defaultAlive"}, // 000c4dac
	"BUFF_ALL_ATTR_DRAIN_DOT_BURN":                           {feature: 234, policy: "defaultAlive"}, // 000c4e28
	"BUFF_ALL_ATTR_DRAIN_DOT_FREEZE":                         {feature: 237, policy: "defaultAlive"}, // 000c4ea4
	"BUFF_ALL_ATTR_DRAIN_DOT_BLEED":                          {feature: 240, policy: "defaultAlive"}, // 000c4f20
	"BUFF_ALL_ATTR_DRAIN_DOT_ELECTRIC":                       {feature: 243, policy: "defaultAlive"}, // 000c4f9c
	"BUFF_SELF_NEED_COST_DOWN_BURST":                         {feature: 244, policy: "defaultAlive"}, // 000c5018
	"BUFF_ONE_RANDOM_NEED_COST_DOWN_BURST":                   {feature: 245, policy: "friendRandom"}, // 000c5094
	"BUFF_ALL_NEED_COST_DOWN_BURST":                          {feature: 246, policy: "defaultAlive"}, // 000c5158
	"BURST_GAUGE_SELF_UP":                                    {feature: 247, policy: "defaultAlive"}, // 000c51d4
	"BURST_GAUGE_ONE_RANDOM_UP":                              {feature: 248, policy: "friendRandom"}, // 000c5250
	"BURST_GAUGE_ALL_UP":                                     {feature: 249, policy: "defaultAlive"}, // 000c5314
	"BUFF_SELF_BURST_GAUGE_REGENE_UP":                        {feature: 250, policy: "defaultAlive"}, // 000c5390
	"BUFF_ONE_RANDOM_BURST_GAUGE_REGENE_UP":                  {feature: 251, policy: "friendRandom"}, // 000c540c
	"BUFF_ALL_BURST_GAUGE_REGENE_UP":                         {feature: 252, policy: "defaultAlive"}, // 000c54d0
	"HEAL_SELF":                                              {feature: 6, policy: "healSelf"},
	"HEAL_LOW_HP_MEMBER":                                     {feature: 7, policy: "healLowHP"},
	"HEAL_LOW_RATE_HP_MEMBER":                                {feature: 7, policy: "healLowRate"},
	"HEAL_ALL":                                               {feature: 8, policy: "healAll"},
	"REGENERATE_SELF":                                        {feature: 12, policy: "regenerateSelf"},
	"REGENERATE_LOW_HP_MEMBER":                               {feature: 13, policy: "regenerateLowHP"},
	"REGENERATE_LOW_RATE_HP_MEMBER":                          {feature: 13, policy: "regenerateLowRate"},
	"REGENERATE_ALL":                                         {feature: 14, policy: "regenerateAll"},
	"BAD_STATUS_ONE_RANDOM_POISON":                           {feature: 90, policy: "enemyMissing", parameter: "POISON"},                          // 000b9a40
	"BAD_STATUS_ONE_RANDOM_BURN":                             {feature: 91, policy: "enemyMissing", parameter: "BURN"},                            // 000b9ba0
	"BAD_STATUS_ONE_RANDOM_FREEZE":                           {feature: 92, policy: "enemyMissing", parameter: "FREEZE"},                          // 000b9d00
	"BAD_STATUS_ONE_RANDOM_BLEED":                            {feature: 93, policy: "enemyMissing", parameter: "BLEED"},                           // 000b9e60
	"CARD_SEAL_RECOVERY_SELF":                                {feature: 9, policy: "cureSelf", parameter: "CARD_SEAL", allyFeature: 9},            // 000afea5
	"CARD_SEAL_RECOVERY_MANY_SEAL_MEMBER":                    {feature: 10, policy: "cureMost", parameter: "CARD_SEAL", allyFeature: 9},           // 000b1fe8
	"CARD_SEAL_RECOVERY_ALL":                                 {feature: 11, policy: "cureAll", parameter: "CARD_SEAL", allyFeature: 9},            // 000b07cc
	"BAD_STATUS_ONE_RANDOM_WEAKNESS":                         {feature: 94, policy: "enemyMissing", parameter: "WEAKNESS"},                        // 000b9fc0
	"DOT_VALUE_UP_RANDOM":                                    {feature: 117, policy: "enemyDOT"},                                                  // 000bfe20
	"DOT_VALUE_UP_POISON_RANDOM":                             {feature: 109, policy: "enemyDOT", parameter: "POISON"},                             // 000c01ce
	"DOT_VALUE_UP_BURN_RANDOM":                               {feature: 111, policy: "enemyDOT", parameter: "BURN"},                               // 000c02c8
	"DOT_VALUE_UP_FREEZE_RANDOM":                             {feature: 113, policy: "enemyDOT", parameter: "FREEZE"},                             // 000c03c2
	"DOT_VALUE_UP_BLEED_RANDOM":                              {feature: 115, policy: "enemyDOT", parameter: "BLEED"},                              // 000c04bc
	"DEBUFF_ONE_WEAKNESS_ATK_DOWN":                           {feature: 55, policy: "enemyWeakness"},                                              // 000b827a
	"DEBUFF_ONE_WEAKNESS_MAX_HP_DOWN":                        {feature: 56, policy: "enemyWeakness"},                                              // 000b835e
	"DEBUFF_ONE_WEAKNESS_INT_DOWN":                           {feature: 57, policy: "enemyWeakness"},                                              // 000b8442
	"DEBUFF_ONE_WEAKNESS_MND_DOWN":                           {feature: 58, policy: "enemyWeakness"},                                              // 000b8526
	"DEBUFF_ONE_WEAKNESS_DEF_DOWN":                           {feature: 59, policy: "enemyWeakness"},                                              // 000b860a
	"DEBUFF_ONE_WEAKNESS_MDEF_DOWN":                          {feature: 60, policy: "enemyWeakness"},                                              // 000b86ee
	"DEBUFF_ONE_WEAKNESS_DAMAGE_DOWN":                        {feature: 61, policy: "enemyWeakness"},                                              // 000b87d2
	"BAD_STATUS_ONE_WEAKNESS_POISON":                         {feature: 90, policy: "enemyWeaknessMissing", parameter: "POISON"},                  // 000bea74
	"BAD_STATUS_ONE_WEAKNESS_BURN":                           {feature: 91, policy: "enemyWeaknessMissing", parameter: "BURN"},                    // 000beb74
	"BAD_STATUS_ONE_WEAKNESS_FREEZE":                         {feature: 92, policy: "enemyWeaknessMissing", parameter: "FREEZE"},                  // 000bec74
	"BAD_STATUS_ONE_WEAKNESS_BLEED":                          {feature: 93, policy: "enemyWeaknessMissing", parameter: "BLEED"},                   // 000bed74
	"BAD_STATUS_ONE_WEAKNESS_WEAKNESS":                       {feature: 94, policy: "enemyWeakness", parameter: "WEAKNESS"},                       // 000bf074
	"BAD_STATUS_ONE_RANDOM_ELECTRIC":                         {feature: 119, policy: "enemyMissing", parameter: "ELECTRIC"},                       // 000ba120
	"DOT_VALUE_UP_ELECTRIC_RANDOM":                           {feature: 121, policy: "enemyDOT", parameter: "ELECTRIC"},                           // 000c05b6
	"BAD_STATUS_ONE_WEAKNESS_ELECTRIC":                       {feature: 119, policy: "enemyWeaknessMissing", parameter: "ELECTRIC"},               // 000bee74
	"DEBUFF_ONE_HIGH_ATK_ATK_DOWN":                           {feature: 55, policy: "enemyHigh", parameter: "ATK"},                                // 000b6a15
	"DEBUFF_ONE_HIGH_MAX_HP_MAX_HP_DOWN":                     {feature: 56, policy: "enemyHigh", parameter: "MAX_HP"},                             // 000b6af2
	"DEBUFF_ONE_HIGH_INT_INT_DOWN":                           {feature: 57, policy: "enemyHigh", parameter: "INT"},                                // 000b6bcf
	"DEBUFF_ONE_HIGH_MND_MND_DOWN":                           {feature: 58, policy: "enemyHigh", parameter: "MND"},                                // 000b6cac
	"DEBUFF_ONE_HIGH_DEF_DEF_DOWN":                           {feature: 59, policy: "enemyHigh", parameter: "DEF"},                                // 000b6d89
	"DEBUFF_ONE_HIGH_MDEF_MDEF_DOWN":                         {feature: 60, policy: "enemyHigh", parameter: "MDEF"},                               // 000b6e66
	"DEBUFF_ONE_HIGH_ATK_DAMAGE_DOWN":                        {feature: 61, policy: "enemyHigh", parameter: "ATK"},                                // 000b6f43
	"DEBUFF_ONE_HIGH_INT_DAMAGE_DOWN":                        {feature: 61, policy: "enemyHigh", parameter: "INT"},                                // 000b7020
	"DEBUFF_ONE_HIGH_DEF_PARTS_DEF_DOWN":                     {feature: 59, policy: "enemyHigh", parameter: "DEF", parts: true},                   // 000b7441
	"DEBUFF_ONE_HIGH_MDEF_PARTS_MDEF_DOWN":                   {feature: 60, policy: "enemyHigh", parameter: "MDEF", parts: true},                  // 000b7512
	"DARKNESS_RECOVERY_SELF":                                 {feature: 123, policy: "cureSelf", parameter: "DARKNESS", allyFeature: 123},         // 000aff6f
	"DARKNESS_RECOVERY_MANY_DARKNESS_MEMBER":                 {feature: 124, policy: "cureMost", parameter: "DARKNESS", allyFeature: 123},         // 000b2104
	"DARKNESS_RECOVERY_ALL":                                  {feature: 125, policy: "cureAll", parameter: "DARKNESS", allyFeature: 123},          // 000b0918
	"BUFF_ONE_HIGH_ATK_CRITICAL_UP":                          {feature: 41, policy: "friendHigh", parameter: "ATK"},                               // 000b5116
	"BUFF_ONE_HIGH_INT_CRITICAL_UP":                          {feature: 41, policy: "friendHigh", parameter: "INT"},                               // 000b51f3
	"BUFF_ONE_HIGH_ATK_ATK_UP":                               {feature: 33, policy: "friendHigh", parameter: "ATK"},                               // 000b4f5c
	"BUFF_ONE_HIGH_INT_INT_UP":                               {feature: 35, policy: "friendHigh", parameter: "INT"},                               // 000b5039
	"BAD_STATUS_ONE_RANDOM_CARD_TRAP_DOT":                    {feature: 126, policy: "enemyMissing", parameter: "CARD_TRAP_DOT"},                  // 000ba280
	"BAD_STATUS_ONE_WEAKNESS_CARD_TRAP_DOT":                  {feature: 126, policy: "enemyWeaknessMissing", parameter: "CARD_TRAP_DOT"},          // 000bef74
	"CARD_TRAP_DAMAGE_RECOVERY_SELF":                         {feature: 130, policy: "cureSelf", parameter: "CARD_TRAP_DAMAGE", allyFeature: 130}, // 000b0045
	"CARD_TRAP_DAMAGE_RECOVERY_MANY_CARD_TRAP_DAMAGE_MEMBER": {feature: 131, policy: "cureMost", parameter: "CARD_TRAP_DAMAGE", allyFeature: 130}, // 000b2250
	"CARD_TRAP_DAMAGE_RECOVERY_ALL":                          {feature: 132, policy: "cureAll", parameter: "CARD_TRAP_DAMAGE", allyFeature: 130},  // 000b0a70
	"COST_BLOCK_RECOVERY_SELF":                               {feature: 133, policy: "cureSelf", parameter: "COST_BLOCK", allyFeature: 133},       // 000b011b
	"COST_BLOCK_RECOVERY_MANY_COST_BLOCK_MEMBER":             {feature: 134, policy: "cureMost", parameter: "COST_BLOCK", allyFeature: 133},       // 000b2378
	"COST_BLOCK_RECOVERY_ALL":                                {feature: 135, policy: "cureAll", parameter: "COST_BLOCK", allyFeature: 133},        // 000b0bc8
	"BUFF_ONE_HIGH_ATK_ENCHANT":                              {feature: 42, policy: "friendHigh", parameter: "ATK"},                               // 000b52d0
	"BUFF_ONE_HIGH_INT_ENCHANT":                              {feature: 42, policy: "friendHigh", parameter: "INT"},                               // 000b53ad
	"BUFF_ONE_MILLIONAIRE_MAX_HP_UP":                         {feature: 34, policy: "friendJob", parameter: "MILLIONAIRE"},                        // 000b589a
	"BUFF_ONE_LOW_HP_MAX_HP_UP":                              {feature: 34, policy: "friendLowHP"},                                                // 000b5644
	"BUFF_ONE_LOW_RATE_HP_MAX_HP_UP":                         {feature: 34, policy: "friendLowRate"},                                              // 000b5720
	"DOT_VALUE_UP_WEAKNESS":                                  {feature: 117, policy: "enemyWeaknessDOT"},                                          // 000c11f0
	"DOT_VALUE_UP_POISON_WEAKNESS":                           {feature: 109, policy: "enemyWeaknessDOT", parameter: "POISON"},                     // 000c1392
	"DOT_VALUE_UP_BURN_WEAKNESS":                             {feature: 111, policy: "enemyWeaknessDOT", parameter: "BURN"},                       // 000c1494
	"DOT_VALUE_UP_FREEZE_WEAKNESS":                           {feature: 113, policy: "enemyWeaknessDOT", parameter: "FREEZE"},                     // 000c1596
	"DOT_VALUE_UP_BLEED_WEAKNESS":                            {feature: 115, policy: "enemyWeaknessDOT", parameter: "BLEED"},                      // 000c1698
	"DOT_VALUE_UP_ELECTRIC_WEAKNESS":                         {feature: 121, policy: "enemyWeaknessDOT", parameter: "ELECTRIC"},                   // 000c179a
	"BUFF_ONE_MILLIONAIRE_ATTR_DEF_UP":                       {feature: 43, policy: "friendJob", parameter: "MILLIONAIRE"},                        // 000b597e
	"DEBUFF_ONE_WEAKNESS_ATTR_DEF_DOWN":                      {feature: 62, policy: "enemyWeakness"},                                              // 000b899a
	"DEBUFF_ONE_LOW_ATTR_RATE_ATTR_DEF_DOWN":                 {feature: 62, policy: "enemyAttributeDefense"},                                      // 000b7790
	"DEBUFF_ONE_LOW_ATTR_RATE_PARTS_ATTR_DEF_DOWN":           {feature: 62, policy: "enemyAttributeDefense", parts: true},                         // 000b7a10
	"POISON_RECOVERY_SELF":                                   {feature: 139, policy: "cureSelf", parameter: "POISON", allyFeature: 139},           // 000b02c7
	"POISON_RECOVERY_RANDOM":                                 {feature: 140, policy: "cureRandom", parameter: "POISON", allyFeature: 139},         // 000b1690
	"POISON_RECOVERY_ALL":                                    {feature: 141, policy: "cureAll", parameter: "POISON", allyFeature: 139},            // 000b0e78
	"GOOD_STATUS_ONE_MILLIONAIRE_REFLECTION":                 {feature: 84, policy: "friendJob", parameter: "MILLIONAIRE"},                        // 000bf4b8
	"CURSE_RECOVERY_SELF_ATTR_FIRE":                          {feature: 157, policy: "curseSelf", parameter: "FIRE", allyFeature: 157},            // 000b2b80
	"CURSE_RECOVERY_RANDOM_ATTR_FIRE":                        {feature: 158, policy: "curseRandom", parameter: "FIRE", allyFeature: 157},          // 000b2fe0
	"CURSE_RECOVERY_ALL_ATTR_FIRE":                           {feature: 159, policy: "curseAll", parameter: "FIRE", allyFeature: 157},             // 000b37b0
	"CURSE_RECOVERY_SELF_ATTR_ICE":                           {feature: 160, policy: "curseSelf", parameter: "ICE", allyFeature: 160},             // 000b2c60
	"CURSE_RECOVERY_RANDOM_ATTR_ICE":                         {feature: 161, policy: "curseRandom", parameter: "ICE", allyFeature: 160},           // 000b3170
	"CURSE_RECOVERY_ALL_ATTR_ICE":                            {feature: 162, policy: "curseAll", parameter: "ICE", allyFeature: 160},              // 000b38e0
	"CURSE_RECOVERY_SELF_ATTR_WIND":                          {feature: 163, policy: "curseSelf", parameter: "WIND", allyFeature: 163},            // 000b2d40
	"CURSE_RECOVERY_RANDOM_ATTR_WIND":                        {feature: 164, policy: "curseRandom", parameter: "WIND", allyFeature: 163},          // 000b3300
	"CURSE_RECOVERY_ALL_ATTR_WIND":                           {feature: 165, policy: "curseAll", parameter: "WIND", allyFeature: 163},             // 000b3a10
	"CURSE_RECOVERY_SELF_ATTR_LIGHT":                         {feature: 166, policy: "curseSelf", parameter: "LIGHT", allyFeature: 166},           // 000b2e20
	"CURSE_RECOVERY_RANDOM_ATTR_LIGHT":                       {feature: 167, policy: "curseRandom", parameter: "LIGHT", allyFeature: 166},         // 000b3490
	"CURSE_RECOVERY_ALL_ATTR_LIGHT":                          {feature: 168, policy: "curseAll", parameter: "LIGHT", allyFeature: 166},            // 000b3b40
	"CURSE_RECOVERY_SELF_ATTR_DARK":                          {feature: 169, policy: "curseSelf", parameter: "DARK", allyFeature: 169},            // 000b2f00
	"CURSE_RECOVERY_RANDOM_ATTR_DARK":                        {feature: 170, policy: "curseRandom", parameter: "DARK", allyFeature: 169},          // 000b3620
	"CURSE_RECOVERY_ALL_ATTR_DARK":                           {feature: 171, policy: "curseAll", parameter: "DARK", allyFeature: 169},             // 000b3c70
	"BUFF_ONE_HIGH_ATK_ATK_LIMIT_BREAK":                      {feature: 191, policy: "friendHigh", parameter: "ATK"},                              // 000b548a
	"BUFF_ONE_HIGH_INT_INT_LIMIT_BREAK":                      {feature: 194, policy: "friendHigh", parameter: "INT"},                              // 000b5567
	"BUFF_ONE_SINGER_MND_LIMIT_BREAK":                        {feature: 197, policy: "friendJob", parameter: "SINGER"},                            // 000b5a62
	"ATTACK_ATTR_RATE_HIGH":                                  {feature: 0, policy: "attackAttribute"},                                             // 000ae298
}
