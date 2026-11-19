package logs

import corev1 "k8s.io/api/core/v1"

func (c *Collector) OnPodAdd(pod *corev1.Pod) {
	c.HandlePod(pod)
}

func (c *Collector) OnPodUpdate(pod *corev1.Pod) {
	c.HandlePod(pod)
}

func (c *Collector) OnPodDelete(pod *corev1.Pod) {
	c.StopPod(pod.Namespace, pod.Name)
}
