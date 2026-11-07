using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Drawing;
using System.IO;
using System.Linq;
using System.Net;
using System.Net.NetworkInformation;
using System.Net.Sockets;
using System.Text;
using System.Threading.Tasks;
using System.Web.Script.Serialization;
using System.Windows.Forms;

namespace KairiseiLauncher
{
    internal sealed class ServerState
    {
        public string status { get; set; }
        public int process_id { get; set; }
        public string advertise_host { get; set; }
        public int port { get; set; }
        public int battle_port { get; set; }
        public int admin_port { get; set; }
        public string admin_url { get; set; }
        public string stdout_path { get; set; }
        public string stderr_path { get; set; }
        public bool IsReady { get; set; }
    }

    internal sealed class DeploymentDefaults
    {
        public string advertise_host { get; set; }
        public int port { get; set; }
        public bool validation_only { get; set; }
    }

    internal static class Program
    {
        [STAThread]
        private static int Main(string[] args)
        {
            if (args.Any(value => String.Equals(value, "--smoke-test", StringComparison.OrdinalIgnoreCase)))
            {
                var root = AppDomain.CurrentDomain.BaseDirectory;
                var resourceSet = Path.Combine(root, "resource-set", "resource-set.json");
                var contentRoot = File.Exists(resourceSet) ? "resource-set" : "";
                var required = new[]
                {
                    "kairi-server.exe", "Start-Server.ps1", "Stop-Server.ps1",
                    Path.Combine(contentRoot, "server", "config", "cn602-save-template.json"),
                    Path.Combine(contentRoot, "_local", "control", "server", "cn602-card-runtime-master.json")
                };
                if (File.Exists(resourceSet) && !File.Exists(Path.Combine(root, "Read-ResourceSet.ps1"))) return 2;
                return required.All(value => File.Exists(Path.Combine(root, value))) ? 0 : 2;
            }

            Application.EnableVisualStyles();
            Application.SetCompatibleTextRenderingDefault(false);
            Application.Run(new LauncherForm());
            return 0;
        }
    }

    internal sealed class LauncherForm : Form
    {
        private static readonly Color Navy = Color.FromArgb(11, 103, 159);
        private static readonly Color TextColor = Color.FromArgb(23, 55, 78);
        private static readonly Color CyanDark = Color.FromArgb(11, 103, 159);
        private static readonly Color Pale = Color.FromArgb(230, 247, 255);
        private static readonly Color Canvas = Color.FromArgb(244, 249, 252);
        private static readonly Color Muted = Color.FromArgb(77, 112, 134);
        private static readonly Color Success = Color.FromArgb(42, 157, 100);
        private static readonly Color Danger = Color.FromArgb(222, 78, 78);

        private readonly string root;
        private readonly ServerController controller;
        private readonly JavaScriptSerializer serializer = new JavaScriptSerializer();
        private readonly Timer timer = new Timer();
        private ComboBox mode;
        private ComboBox address;
        private NumericUpDown port;
        private Label hint;
        private Label stateBadge;
        private Label endpoints;
        private TextBox activity;
        private Button startButton;
        private Button stopButton;
        private Button adminButton;
        private string packagedHost = "127.0.0.1";
        private int packagedPort = 26040;
        private bool validationOnly;
        private bool busy;
        private bool refreshing;
        private ServerState latestState;
        private string operationMessage = "";

        public LauncherForm()
        {
            root = AppDomain.CurrentDomain.BaseDirectory.TrimEnd(Path.DirectorySeparatorChar);
            controller = new ServerController(root);
            BuildUI();
            LoadDeploymentDefaults();
            Shown += async delegate
            {
                PopulateAddresses();
                await RefreshStatus(true);
                if (IsDisposed || Disposing) return;
                timer.Interval = 1500;
                timer.Tick += async delegate { await RefreshStatus(false); };
                timer.Start();
            };
            FormClosing += delegate { timer.Stop(); };
        }

        private void BuildUI()
        {
            Text = "乖离性MA · 服务端启动器";
            StartPosition = FormStartPosition.CenterScreen;
            ClientSize = new Size(1040, 675);
            MinimumSize = new Size(1056, 714);
            Font = new Font("Microsoft YaHei UI", 10F);
            BackColor = Canvas;
            AutoScaleMode = AutoScaleMode.None;
            try { Icon = Icon.ExtractAssociatedIcon(Application.ExecutablePath); } catch { }

            var header = new Panel
            {
                Location = new Point(0, 0), Size = new Size(1040, 112),
                Anchor = AnchorStyles.Top | AnchorStyles.Left | AnchorStyles.Right,
                BackColor = Canvas
            };
            Controls.Add(header);
            var iconBox = new PictureBox { Location = new Point(28, 21), Size = new Size(68, 68), SizeMode = PictureBoxSizeMode.Zoom };
            try { iconBox.Image = Icon.ToBitmap(); } catch { }
            header.Controls.Add(iconBox);
            header.Controls.Add(new Label
            {
                Text = "乖离性MA",
                Location = new Point(116, 20), AutoSize = true,
                Font = new Font("Microsoft YaHei UI", 20F, FontStyle.Bold), ForeColor = Navy
            });
            header.Controls.Add(new Label
            {
                Text = "Windows x64",
                Location = new Point(119, 78), AutoSize = true,
                ForeColor = Muted
            });
            var sourceLink = new LinkLabel
            {
                Text = "本项目永久免费 · 从 GitHub 获取",
                Font = new Font("Microsoft YaHei UI", 10F, FontStyle.Bold),
                LinkColor = Color.FromArgb(220, 91, 48),
                ActiveLinkColor = Danger,
                AutoSize = true,
                Location = new Point(490, 35)
            };
            sourceLink.LinkClicked += delegate
            {
                try
                {
                    Process.Start(new ProcessStartInfo("https://github.com/kuuhaku1314/kairisei-ma-ch") { UseShellExecute = true });
                }
                catch (Exception ex) { ShowFailure("打开项目主页失败：" + ex.Message); }
            };
            header.Controls.Add(sourceLink);
            stateBadge = new Label
            {
                Text = "● 检查中", TextAlign = ContentAlignment.MiddleCenter,
                Location = new Point(855, 34), Size = new Size(150, 42),
                Anchor = AnchorStyles.Top | AnchorStyles.Right,
                Font = new Font("Microsoft YaHei UI", 10F, FontStyle.Bold),
                ForeColor = Navy, BackColor = Canvas
            };
            header.Controls.Add(stateBadge);

            var settings = Group("连接方式", new Point(24, 132), new Size(992, 204));
            settings.Controls.Add(LabelAt("使用场景", 22, 40));
            mode = new ComboBox
            {
                DropDownStyle = ComboBoxStyle.DropDownList,
                Location = new Point(160, 31), Size = new Size(260, 34)
            };
            mode.Items.AddRange(new object[] { "本机", "局域网", "公网 IP / 域名" });
            mode.SelectedIndex = 0;
            mode.SelectedIndexChanged += delegate { ApplyMode(); };
            settings.Controls.Add(mode);
            settings.Controls.Add(LabelAt("服务器地址", 22, 87));
            address = new ComboBox
            {
                DropDownStyle = ComboBoxStyle.DropDown,
                Location = new Point(160, 78), Size = new Size(215, 34), Text = "127.0.0.1"
            };
            address.TextChanged += delegate { UpdateClientConfig(); };
            settings.Controls.Add(address);
            var restoreDefault = ButtonAt("恢复默认", 388, 78, 120, 34, Pale, CyanDark);
            restoreDefault.Click += delegate { RestoreDeploymentDefaults(); };
            settings.Controls.Add(restoreDefault);
            settings.Controls.Add(LabelAt("主端口", 22, 134));
            port = new NumericUpDown
            {
                Location = new Point(160, 125), Size = new Size(140, 34),
                Minimum = 1, Maximum = 65533, Value = 26040
            };
            port.ValueChanged += delegate { UpdateClientConfig(); };
            settings.Controls.Add(port);
            settings.Controls.Add(new Label
            {
                Text = "战斗端口 = 主端口 + 1；Admin = 主端口 + 2（仅本机）",
                Location = new Point(330, 130), Size = new Size(630, 30), ForeColor = Muted
            });
            hint = new Label
            {
                Location = new Point(535, 30), Size = new Size(425, 82),
                ForeColor = Muted
            };
            settings.Controls.Add(hint);

            var actions = Group("服务控制", new Point(24, 350), new Size(992, 130));
            startButton = ButtonAt("启动服务", 22, 37, 188, 45, Color.FromArgb(20, 148, 211), Color.White);
            stopButton = ButtonAt("停止服务", 220, 37, 188, 45, Danger, Color.White);
            adminButton = ButtonAt("打开 Admin", 418, 37, 188, 45, Pale, CyanDark);
            var logsButton = ButtonAt("查看日志", 616, 37, 210, 45, Pale, Navy);
            startButton.Click += async delegate { await StartServer(); };
            stopButton.Click += async delegate { await StopServer(); };
            adminButton.Click += delegate { OpenAdmin(); };
            logsButton.Click += delegate { OpenLogs(); };
            actions.Controls.AddRange(new Control[] { startButton, stopButton, adminButton, logsButton });
            endpoints = new Label
            {
                Text = "游戏 26040  ·  战斗 26041  ·  Admin 127.0.0.1:26042",
                Location = new Point(23, 93), Size = new Size(800, 25), ForeColor = Muted
            };
            actions.Controls.Add(endpoints);

            activity = new TextBox
            {
                Location = new Point(24, 500), Size = new Size(992, 128),
                Anchor = AnchorStyles.Top | AnchorStyles.Bottom | AnchorStyles.Left | AnchorStyles.Right,
                Multiline = true, ReadOnly = true, ScrollBars = ScrollBars.Vertical,
                BackColor = Color.White, ForeColor = TextColor, BorderStyle = BorderStyle.FixedSingle
            };
            Controls.Add(activity);
            Controls.Add(new Label
            {
                Text = "请完整解压后运行；首次启动会创建全新本地存档。",
                Location = new Point(25, 640), AutoSize = true, ForeColor = Muted,
                Anchor = AnchorStyles.Bottom | AnchorStyles.Left
            });

            ApplyMode();
            AutoScaleDimensions = new SizeF(96F, 96F);
            AutoScaleMode = AutoScaleMode.Dpi;
            PerformAutoScale();
        }

        private GroupBox Group(string title, Point location, Size size)
        {
            var group = new GroupBox
            {
                Text = "  " + title + "  ", Location = location, Size = size,
                BackColor = Color.White, ForeColor = TextColor,
                Anchor = AnchorStyles.Top | AnchorStyles.Left | AnchorStyles.Right
            };
            Controls.Add(group);
            return group;
        }

        private static Label LabelAt(string text, int x, int y)
        {
            return new Label { Text = text, Location = new Point(x, y), AutoSize = true, ForeColor = TextColor };
        }

        private static Button ButtonAt(string text, int x, int y, int width, int height, Color back, Color fore)
        {
            var button = new Button
            {
                Text = text, Location = new Point(x, y), Size = new Size(width, height),
                FlatStyle = FlatStyle.Flat, BackColor = back, ForeColor = fore,
                Cursor = Cursors.Hand, UseVisualStyleBackColor = false
            };
            button.FlatAppearance.BorderSize = 0;
            return button;
        }

        private void PopulateAddresses()
        {
            var candidates = new SortedSet<string>(StringComparer.OrdinalIgnoreCase);
            try
            {
                foreach (var network in NetworkInterface.GetAllNetworkInterfaces())
                {
                    if (network.OperationalStatus != OperationalStatus.Up || network.NetworkInterfaceType == NetworkInterfaceType.Loopback) continue;
                    foreach (var item in network.GetIPProperties().UnicastAddresses)
                    {
                        if (item.Address.AddressFamily == AddressFamily.InterNetwork)
                            candidates.Add(item.Address.ToString());
                    }
                }
            }
            catch { }
            foreach (var candidate in candidates) address.Items.Add(candidate);
        }

        private void LoadDeploymentDefaults()
        {
            var path = Path.Combine(root, "deployment.json");
            if (!File.Exists(path)) return;
            try
            {
                var defaults = serializer.Deserialize<DeploymentDefaults>(File.ReadAllText(path, Encoding.UTF8));
                var parsed = defaults == null ? null : NormalizeHost(defaults.advertise_host);
                if (parsed == null || defaults.port < 1 || defaults.port > 65533) return;
                packagedHost = parsed;
                packagedPort = defaults.port;
                validationOnly = defaults.validation_only;
                RestoreDeploymentDefaults();
            }
            catch { }
        }

        private void RestoreDeploymentDefaults()
        {
            var host = NormalizeHost(packagedHost);
            if (host == null) return;
            mode.SelectedIndex = ModeForHost(host);
            address.Text = host;
            port.Value = Math.Max(port.Minimum, Math.Min(port.Maximum, packagedPort));
            UpdateClientConfig();
        }

        private static bool IsPrivate(IPAddress value)
        {
            var bytes = value.GetAddressBytes();
            return bytes.Length == 4 && (bytes[0] == 10 || bytes[0] == 127 ||
                (bytes[0] == 172 && bytes[1] >= 16 && bytes[1] <= 31) ||
                (bytes[0] == 192 && bytes[1] == 168));
        }

        private static int ModeForHost(string host)
        {
            var parsed = ParseIPv4(host);
            if (host == "localhost" || (parsed != null && IPAddress.IsLoopback(parsed))) return 0;
            return parsed != null && IsPrivate(parsed) ? 1 : 2;
        }

        private void ApplyMode()
        {
            if (mode.SelectedIndex == 0)
            {
                address.Text = "127.0.0.1";
                address.Enabled = false;
                hint.Text = "供同一台电脑上的 PC 客户端连接。\r\n客户端填写 127.0.0.1 和下方主端口。";
            }
            else if (mode.SelectedIndex == 1)
            {
                address.Enabled = true;
                var current = ParseIPv4(address.Text);
                if (NormalizeHost(address.Text) == null || (current != null && IPAddress.IsLoopback(current)) || address.Text == "localhost")
                    address.Text = address.Items.Count > 0 ? address.Items[0].ToString() : "192.168.1.100";
                hint.Text = "填写客户端可访问的 IPv4 或域名，支持虚拟局域网及内网穿透。\r\n需放行或映射 TCP 主端口和战斗端口。";
            }
            else
            {
                address.Enabled = true;
                hint.Text = "填写这台服务器的公网 IPv4 或域名。\r\n需映射 TCP 主端口与战斗端口；Admin 不会公网开放。";
            }
            UpdateClientConfig();
        }

        private static IPAddress ParseIPv4(string text)
        {
            IPAddress parsed;
            return IPAddress.TryParse((text ?? "").Trim(), out parsed) && parsed.AddressFamily == AddressFamily.InterNetwork ? parsed : null;
        }

        private string ValidateAddress()
        {
            var host = NormalizeHost(address.Text);
            if (host == null) throw new InvalidOperationException("连接地址必须是有效的 IPv4 或域名；端口请在下方填写。");
            return host;
        }

        private static string NormalizeHost(string text)
        {
            string host = (text ?? "").Trim().ToLowerInvariant();
            if (host.Length == 0 || host.Length > 253) return null;
            if (System.Text.RegularExpressions.Regex.IsMatch(host, "^[0-9.]+$"))
            {
                if (!System.Text.RegularExpressions.Regex.IsMatch(host, @"^(?:0|[1-9][0-9]{0,2})(?:\.(?:0|[1-9][0-9]{0,2})){3}$")) return null;
                var ip = ParseIPv4(host);
                if (ip == null || ip.GetAddressBytes()[0] == 0 || ip.GetAddressBytes()[0] >= 224) return null;
            }
            else foreach (string label in host.Split('.'))
                if (!System.Text.RegularExpressions.Regex.IsMatch(label, "^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$")) return null;
            return host;
        }

        private void UpdateClientConfig()
        {
            if (port == null || address == null) return;
            if (endpoints != null)
            {
                var basePort = Decimal.ToInt32(port.Value);
                endpoints.Text = "游戏 " + basePort + "  ·  战斗 " + (basePort + 1) + "  ·  Admin 127.0.0.1:" + (basePort + 2);
            }
        }

        private async Task StartServer()
        {
            if (busy) return;
            try
            {
                var host = ValidateAddress();
                var basePort = Decimal.ToInt32(port.Value);
                if (mode.SelectedIndex == 2)
                {
                    var answer = MessageBox.Show(this,
                        "公网联机需要你在路由器或云防火墙映射 TCP " + basePort + " 和 " + (basePort + 1) + "。\r\n" +
                        "Admin 端口不会对外开放。继续启动吗？",
                        "公网联机提示", MessageBoxButtons.OKCancel, MessageBoxIcon.Information);
                    if (answer != DialogResult.OK) return;
                }
                await RunOperation("正在启动服务……", () => controller.StartAsync(host, basePort, validationOnly));
            }
            catch (Exception ex) { ShowFailure(ex.Message); }
        }

        private async Task StopServer()
        {
            if (busy) return;
            await RunOperation("正在停止服务……", () => controller.StopAsync());
        }

        private async Task RunOperation(string message, Func<Task<OperationResult>> action)
        {
            if (busy) return;
            busy = true;
            operationMessage = message;
            ApplyState(latestState, false);
            SetBusy(true);
            try
            {
                var result = await action();
                if (IsDisposed || Disposing) return;
                operationMessage = result.exit_code == 0 ? "服务操作已完成。" :
                    (String.IsNullOrWhiteSpace(result.output) ? "操作失败，退出码 " + result.exit_code : result.output.Trim());
                if (result.exit_code != 0) ShowFailure(operationMessage + "\r\n日志：" + result.diagnostic_path);
            }
            catch (Exception ex)
            {
                if (!IsDisposed && !Disposing) ShowFailure(ex.Message);
            }
            finally
            {
                busy = false;
                if (!IsDisposed && !Disposing) SetBusy(false);
            }
            await RefreshStatus(false);
        }

        private static string Quote(string value)
        {
            if (value == null) return "\"\"";
            if (value.IndexOf('"') >= 0) throw new InvalidOperationException("参数中不能包含双引号。");
            return "\"" + value + "\"";
        }

        private async Task RefreshStatus(bool initial)
        {
            if (refreshing || IsDisposed || Disposing) return;
            refreshing = true;
            try
            {
                latestState = await controller.ReadStateAsync();
                if (IsDisposed || Disposing) return;
                ApplyState(latestState, initial);
            }
            finally { refreshing = false; }
        }

        private void ApplyState(ServerState state, bool initial)
        {
            var running = state != null && state.IsReady;
            stateBadge.Text = running ? "● 正在运行" : state != null ? "● 检测中" : busy ? "● 操作中" : "● 未运行";
            stateBadge.ForeColor = running ? Success : Muted;
            SetBusy(busy);
            if (running)
            {
                endpoints.Text = "游戏 " + state.port + "  ·  战斗 " + state.battle_port + "  ·  Admin 127.0.0.1:" + state.admin_port;
                if (initial)
                {
                    port.Value = Math.Max(port.Minimum, Math.Min(port.Maximum, state.port));
                    var runningHost = NormalizeHost(state.advertise_host);
                    if (runningHost != null)
                    {
                        mode.SelectedIndex = ModeForHost(runningHost);
                        address.Text = runningHost;
                    }
                }
                activity.Text = operationMessage + "\r\n服务运行中（PID " + state.process_id + "）\r\n客户端填写：" + state.advertise_host + ":" + state.port;
            }
            else
            {
                activity.Text = operationMessage + (state != null ? "\r\n服务进程存在，正在确认健康状态。" :
                    busy ? "" : "\r\n服务尚未运行。选择连接方式后点击“启动服务”。");
            }
        }

        private void SetBusy(bool value)
        {
            startButton.Enabled = !value && latestState == null;
            stopButton.Enabled = !value && latestState != null;
            adminButton.Enabled = latestState != null && latestState.IsReady;
            mode.Enabled = !value;
            address.Enabled = !value && mode.SelectedIndex != 0;
            port.Enabled = !value;
        }

        private void OpenAdmin()
        {
            if (latestState == null || !latestState.IsReady)
            {
                MessageBox.Show(this, "服务尚未运行，请先点击“启动服务”。", "Admin",
                    MessageBoxButtons.OK, MessageBoxIcon.Information);
                return;
            }
            var adminPort = latestState != null && latestState.admin_port > 0
                ? latestState.admin_port : Decimal.ToInt32(port.Value) + 2;
            try { Process.Start(new ProcessStartInfo("http://127.0.0.1:" + adminPort + "/") { UseShellExecute = true }); }
            catch (Exception ex) { ShowFailure("打开 Admin 失败：" + ex.Message); }
        }

        private void OpenLogs()
        {
            var path = Path.Combine(root, "_local", "runtime");
            try
            {
                Directory.CreateDirectory(path);
                Process.Start(new ProcessStartInfo("explorer.exe", Quote(path)) { UseShellExecute = true });
            }
            catch (Exception ex) { ShowFailure("打开日志目录失败：" + ex.Message); }
        }

        private void ShowFailure(string message)
        {
            operationMessage = "失败：" + message;
            activity.Text = operationMessage;
            MessageBox.Show(this, message, "乖离性服务端启动器", MessageBoxButtons.OK, MessageBoxIcon.Error);
        }
    }
}
